package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Client is the agent client for communicating with fastcp-agent
type Client struct {
	socketPath string
	conn       net.Conn
	mu         sync.Mutex
	connected  atomic.Bool
}

// NewClient creates a new agent client
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
	}
}

// Connect connects to the agent socket
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}

	c.conn = conn
	c.connected.Store(true)
	return nil
}

// Close closes the connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected.Store(false)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// call makes an RPC call to the agent
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !c.connected.Load() {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Create request
	req := Request{
		ID:     uuid.New().String(),
		Method: method,
		Params: params,
	}

	// Set deadline
	if deadline, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(deadline)
	} else {
		c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	// Send request
	encoder := json.NewEncoder(c.conn)
	if err := encoder.Encode(req); err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(c.conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}

	// Marshal result back to JSON for caller to unmarshal
	result, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return result, nil
}

// callIsolated makes a one-off RPC call using a dedicated socket connection.
// This is used for long-running operations to avoid blocking the shared client connection.
func (c *Client) callIsolated(ctx context.Context, method string, params any, defaultTimeout time.Duration) (json.RawMessage, error) {
	req := Request{
		ID:     uuid.New().String(),
		Method: method,
		Params: params,
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(defaultTimeout))
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}

	result, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return result, nil
}

// Site operations
func (c *Client) CreateSiteDirectory(ctx context.Context, req *CreateSiteDirectoryRequest) error {
	_, err := c.call(ctx, "site.createDirectory", req)
	return err
}

func (c *Client) DeleteSiteDirectory(ctx context.Context, req *DeleteSiteDirectoryRequest) error {
	_, err := c.call(ctx, "site.deleteDirectory", req)
	return err
}

func (c *Client) InstallWordPress(ctx context.Context, req *InstallWordPressRequest) error {
	_, err := c.call(ctx, "site.installWordPress", req)
	return err
}

func (c *Client) ReloadCaddy(ctx context.Context) error {
	// Regeneration can restart/reload multiple PHP-FPM services and may exceed
	// the default 30s shared RPC timeout under load.
	_, err := c.callIsolated(ctx, "caddy.reload", nil, 2*time.Minute)
	return err
}

// RegenerateCaddyfile is an alias for ReloadCaddy that regenerates the Caddyfile
func (c *Client) RegenerateCaddyfile(ctx context.Context) error {
	return c.ReloadCaddy(ctx)
}

// Database operations
func (c *Client) CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) error {
	_, err := c.call(ctx, "database.create", req)
	return err
}

func (c *Client) DeleteDatabase(ctx context.Context, req *DeleteDatabaseRequest) error {
	_, err := c.call(ctx, "database.delete", req)
	return err
}

func (c *Client) ResetDatabasePassword(ctx context.Context, req *ResetDatabasePasswordRequest) error {
	_, err := c.call(ctx, "database.resetPassword", req)
	return err
}

// SSH key operations
func (c *Client) AddSSHKey(ctx context.Context, req *AddSSHKeyRequest) error {
	_, err := c.call(ctx, "ssh.addKey", req)
	return err
}

func (c *Client) RemoveSSHKey(ctx context.Context, req *RemoveSSHKeyRequest) error {
	_, err := c.call(ctx, "ssh.removeKey", req)
	return err
}

// System operations
func (c *Client) GetSystemStatus(ctx context.Context) (*SystemStatus, error) {
	result, err := c.call(ctx, "system.status", nil)
	if err != nil {
		return nil, err
	}

	var status SystemStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status: %w", err)
	}
	return &status, nil
}

func (c *Client) GetServices(ctx context.Context) ([]*ServiceStatus, error) {
	result, err := c.call(ctx, "system.services", nil)
	if err != nil {
		return nil, err
	}

	var services []*ServiceStatus
	if err := json.Unmarshal(result, &services); err != nil {
		return nil, fmt.Errorf("failed to unmarshal services: %w", err)
	}
	return services, nil
}

// MySQL config operations
func (c *Client) GetMySQLConfig(ctx context.Context) (*MySQLConfig, error) {
	result, err := c.call(ctx, "system.getMysqlConfig", nil)
	if err != nil {
		return nil, err
	}
	var cfg MySQLConfig
	if err := json.Unmarshal(result, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mysql config: %w", err)
	}
	return &cfg, nil
}

func (c *Client) SetMySQLConfig(ctx context.Context, cfg *MySQLConfig) error {
	_, err := c.call(ctx, "system.setMysqlConfig", cfg)
	return err
}

func (c *Client) GetSSHConfig(ctx context.Context) (*SSHConfig, error) {
	result, err := c.call(ctx, "system.getSshConfig", nil)
	if err != nil {
		return nil, err
	}
	var cfg SSHConfig
	if err := json.Unmarshal(result, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ssh config: %w", err)
	}
	return &cfg, nil
}

func (c *Client) SetSSHConfig(ctx context.Context, cfg *SSHConfig) error {
	_, err := c.call(ctx, "system.setSshConfig", cfg)
	return err
}

func (c *Client) GetPHPDefaultConfig(ctx context.Context) (*PHPDefaultConfig, error) {
	result, err := c.call(ctx, "system.getPhpDefaultConfig", nil)
	if err != nil {
		return nil, err
	}
	var cfg PHPDefaultConfig
	if err := json.Unmarshal(result, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal php default config: %w", err)
	}
	return &cfg, nil
}

func (c *Client) SetPHPDefaultConfig(ctx context.Context, cfg *PHPDefaultConfig) error {
	_, err := c.call(ctx, "system.setPhpDefaultConfig", cfg)
	return err
}

func (c *Client) InstallPHPVersion(ctx context.Context, version string) error {
	_, err := c.callIsolated(ctx, "system.installPhpVersion", &PHPVersionInstallRequest{Version: version}, 90*time.Minute)
	return err
}

func (c *Client) GetCaddyConfig(ctx context.Context) (*CaddyConfig, error) {
	result, err := c.call(ctx, "system.getCaddyConfig", nil)
	if err != nil {
		return nil, err
	}
	var cfg CaddyConfig
	if err := json.Unmarshal(result, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal caddy config: %w", err)
	}
	return &cfg, nil
}

func (c *Client) SetCaddyConfig(ctx context.Context, cfg *CaddyConfig) error {
	_, err := c.call(ctx, "system.setCaddyConfig", cfg)
	return err
}

func (c *Client) GetFirewallStatus(ctx context.Context) (*FirewallStatus, error) {
	result, err := c.call(ctx, "system.getFirewallStatus", nil)
	if err != nil {
		return nil, err
	}
	var status FirewallStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal firewall status: %w", err)
	}
	return &status, nil
}

func (c *Client) InstallFirewall(ctx context.Context) error {
	_, err := c.call(ctx, "system.installFirewall", nil)
	return err
}

func (c *Client) SetFirewallEnabled(ctx context.Context, enabled bool) error {
	_, err := c.call(ctx, "system.setFirewallEnabled", map[string]bool{"enabled": enabled})
	return err
}

func (c *Client) FirewallAllowPort(ctx context.Context, req *FirewallRuleRequest) error {
	_, err := c.call(ctx, "system.firewallAllowPort", req)
	return err
}

func (c *Client) FirewallDenyPort(ctx context.Context, req *FirewallRuleRequest) error {
	_, err := c.call(ctx, "system.firewallDenyPort", req)
	return err
}

func (c *Client) FirewallDeleteRule(ctx context.Context, req *FirewallRuleRequest) error {
	_, err := c.call(ctx, "system.firewallDeleteRule", req)
	return err
}

func (c *Client) GetRcloneStatus(ctx context.Context) (*RcloneStatus, error) {
	result, err := c.call(ctx, "system.getRcloneStatus", nil)
	if err != nil {
		return nil, err
	}
	var status RcloneStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rclone status: %w", err)
	}
	return &status, nil
}

func (c *Client) InstallRclone(ctx context.Context) (*RcloneStatus, error) {
	result, err := c.callIsolated(ctx, "system.installRclone", nil, 20*time.Minute)
	if err != nil {
		return nil, err
	}
	var status RcloneStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rclone install status: %w", err)
	}
	return &status, nil
}

// User operations
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) error {
	_, err := c.call(ctx, "user.create", req)
	return err
}

func (c *Client) DeleteUser(ctx context.Context, req *DeleteUserRequest) error {
	_, err := c.call(ctx, "user.delete", req)
	return err
}

func (c *Client) UpdateUserLimits(ctx context.Context, req *UpdateUserLimitsRequest) error {
	_, err := c.call(ctx, "user.updateLimits", req)
	return err
}

// Update operations
func (c *Client) PerformUpdate(ctx context.Context, targetVersion string) error {
	_, err := c.call(ctx, "system.update", &PerformUpdateRequest{TargetVersion: targetVersion})
	return err
}

// Cron operations
func (c *Client) SyncCronJobs(ctx context.Context, req *SyncCronJobsRequest) error {
	_, err := c.call(ctx, "cron.sync", req)
	return err
}
