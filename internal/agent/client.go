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
	_, err := c.call(ctx, "caddy.reload", nil)
	return err
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

// User operations
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) error {
	_, err := c.call(ctx, "user.create", req)
	return err
}

func (c *Client) DeleteUser(ctx context.Context, req *DeleteUserRequest) error {
	_, err := c.call(ctx, "user.delete", req)
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
