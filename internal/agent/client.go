package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
)

const (
	maxMessageSize     = 32 << 20
	defaultCallTimeout = 60 * time.Second
)

// Error is an error reported by the agent.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrUnavailable is returned when the agent socket cannot be reached.
var ErrUnavailable = errors.New("agent unavailable")

// ErrorCode returns the agent error code of err, or "" if err did not come
// from the agent.
func ErrorCode(err error) string {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// Client talks to fastcp-agent. It is safe for concurrent use; every call
// uses its own connection so slow operations never block others.
type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Call invokes method with params and decodes the result into out (which
// may be nil). Without a context deadline the call times out after a
// minute.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode %s params: %w", method, err)
		}
		raw = b
	}
	req := Request{ID: uuid.NewString(), Method: method, Params: raw}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("agent %s: send: %w", method, ctxErr(ctx, err))
	}
	var resp Response
	if err := json.NewDecoder(io.LimitReader(conn, maxMessageSize)).Decode(&resp); err != nil {
		return fmt.Errorf("agent %s: read: %w", method, ctxErr(ctx, err))
	}
	if resp.ID != req.ID {
		return fmt.Errorf("agent %s: response id mismatch", method)
	}
	if resp.Error != "" {
		return &Error{Code: resp.Code, Message: resp.Error}
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("agent %s: decode result: %w", method, err)
		}
	}
	return nil
}

func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (c *Client) callTimeout(ctx context.Context, timeout time.Duration, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Call(ctx, method, params, out)
}

// ---------------------------------------------------------------------------
// Typed helpers

func (c *Client) Ping(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	err := c.callTimeout(ctx, 5*time.Second, "ping", nil, &out)
	return out.Version, err
}

func (c *Client) ApplyConfig(ctx context.Context, st *State) (*ApplyResult, error) {
	var out ApplyResult
	err := c.callTimeout(ctx, 3*time.Minute, "config.apply", st, &out)
	return &out, err
}

func (c *Client) VerifyPassword(ctx context.Context, username, password string) error {
	return c.callTimeout(ctx, 20*time.Second, "auth.verify", &AuthRequest{Username: username, Password: password}, nil)
}

func (c *Client) ChangePassword(ctx context.Context, username, password string) error {
	return c.callTimeout(ctx, 20*time.Second, "auth.changePassword", &AuthRequest{Username: username, Password: password}, nil)
}

func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) error {
	return c.callTimeout(ctx, 2*time.Minute, "user.create", req, nil)
}

func (c *Client) DeleteUser(ctx context.Context, req *DeleteUserRequest) error {
	return c.callTimeout(ctx, 5*time.Minute, "user.delete", req, nil)
}

// UserExec runs script with bash as a registered tenant.
func (c *Client) UserExec(ctx context.Context, req *UserExecRequest) (*UserExecResult, error) {
	timeout := time.Duration(req.TimeoutSec)*time.Second + 30*time.Second
	if req.TimeoutSec <= 0 {
		timeout = 10 * time.Minute
	}
	var out UserExecResult
	err := c.callTimeout(ctx, timeout, "user.exec", req, &out)
	return &out, err
}

func (c *Client) PrepareSpool(ctx context.Context, username string) (string, error) {
	var out SpoolResult
	err := c.Call(ctx, "backup.prepareSpool", &SpoolRequest{Username: username}, &out)
	return out.Dir, err
}

func (c *Client) CreateSite(ctx context.Context, req *CreateSiteRequest) error {
	return c.callTimeout(ctx, 2*time.Minute, "site.create", req, nil)
}

func (c *Client) DeleteSite(ctx context.Context, req *DeleteSiteRequest) error {
	return c.callTimeout(ctx, 10*time.Minute, "site.delete", req, nil)
}

func (c *Client) InstallWordPress(ctx context.Context, req *InstallWordPressRequest) error {
	return c.callTimeout(ctx, 10*time.Minute, "site.installWordPress", req, nil)
}

func (c *Client) ReadSiteLog(ctx context.Context, req *SiteLogRequest) (*SiteLogResult, error) {
	var out SiteLogResult
	if err := c.callTimeout(ctx, 30*time.Second, "site.readLog", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) error {
	return c.Call(ctx, "database.create", req, nil)
}

func (c *Client) DeleteDatabase(ctx context.Context, req *DatabaseTarget) error {
	return c.Call(ctx, "database.delete", req, nil)
}

func (c *Client) ResetDatabasePassword(ctx context.Context, req *ResetDatabasePasswordRequest) error {
	return c.Call(ctx, "database.resetPassword", req, nil)
}

func (c *Client) GetMySQLConfig(ctx context.Context) (*MySQLConfig, error) {
	var out MySQLConfig
	err := c.Call(ctx, "mysql.getConfig", nil, &out)
	return &out, err
}

func (c *Client) SetMySQLConfig(ctx context.Context, cfg *MySQLConfig) error {
	return c.callTimeout(ctx, 3*time.Minute, "mysql.setConfig", cfg, nil)
}

func (c *Client) AddSSHKey(ctx context.Context, req *AddSSHKeyRequest) error {
	return c.Call(ctx, "ssh.addKey", req, nil)
}

func (c *Client) RemoveSSHKey(ctx context.Context, req *RemoveSSHKeyRequest) error {
	return c.Call(ctx, "ssh.removeKey", req, nil)
}

func (c *Client) GetSSHConfig(ctx context.Context) (*SSHConfig, error) {
	var out SSHConfig
	err := c.Call(ctx, "ssh.getConfig", nil, &out)
	return &out, err
}

func (c *Client) SetSSHConfig(ctx context.Context, cfg *SSHConfig) error {
	return c.callTimeout(ctx, 2*time.Minute, "ssh.setConfig", cfg, nil)
}

func (c *Client) SystemStatus(ctx context.Context) (*SystemStatus, error) {
	var out SystemStatus
	err := c.callTimeout(ctx, 15*time.Second, "system.status", nil, &out)
	return &out, err
}

func (c *Client) Services(ctx context.Context) ([]ServiceStatus, error) {
	var out []ServiceStatus
	err := c.callTimeout(ctx, 15*time.Second, "system.services", nil, &out)
	return out, err
}

func (c *Client) GetCaddyConfig(ctx context.Context) (*CaddyConfig, error) {
	var out CaddyConfig
	err := c.Call(ctx, "caddy.getConfig", nil, &out)
	return &out, err
}

func (c *Client) SetCaddyConfig(ctx context.Context, cfg *CaddyConfig) error {
	return c.callTimeout(ctx, 2*time.Minute, "caddy.setConfig", cfg, nil)
}

func (c *Client) FirewallStatus(ctx context.Context) (*FirewallStatus, error) {
	var out FirewallStatus
	err := c.Call(ctx, "firewall.status", nil, &out)
	return &out, err
}

func (c *Client) InstallFirewall(ctx context.Context) (*Job, error) {
	var out Job
	err := c.Call(ctx, "firewall.install", nil, &out)
	return &out, err
}

func (c *Client) SetFirewallEnabled(ctx context.Context, enabled bool) error {
	return c.callTimeout(ctx, 2*time.Minute, "firewall.setEnabled", map[string]bool{"enabled": enabled}, nil)
}

func (c *Client) FirewallRule(ctx context.Context, action string, req *FirewallRuleRequest) error {
	return c.Call(ctx, "firewall."+action, req, nil)
}

func (c *Client) RcloneStatus(ctx context.Context) (*ToolStatus, error) {
	var out ToolStatus
	err := c.Call(ctx, "rclone.status", nil, &out)
	return &out, err
}

func (c *Client) InstallRclone(ctx context.Context) (*Job, error) {
	var out Job
	err := c.Call(ctx, "rclone.install", nil, &out)
	return &out, err
}

func (c *Client) PHPStatus(ctx context.Context) (*PHPStatus, error) {
	var out PHPStatus
	err := c.callTimeout(ctx, 30*time.Second, "php.status", nil, &out)
	return &out, err
}

func (c *Client) InstallPHP(ctx context.Context, version string) (*Job, error) {
	var out Job
	err := c.Call(ctx, "php.install", &PHPVersionRequest{Version: version}, &out)
	return &out, err
}

func (c *Client) RunPHPMaintenance(ctx context.Context) (*Job, error) {
	var out Job
	err := c.Call(ctx, "php.runMaintenance", nil, &out)
	return &out, err
}

func (c *Client) SetPHPSettings(ctx context.Context, s *PHPSettings) error {
	return c.Call(ctx, "php.setSettings", s, nil)
}

func (c *Client) GetJob(ctx context.Context, id string) (*Job, error) {
	var out Job
	err := c.Call(ctx, "job.get", &JobRequest{ID: id}, &out)
	return &out, err
}

func (c *Client) ApplyUpdate(ctx context.Context, version string) (*Job, error) {
	var out Job
	err := c.callTimeout(ctx, 5*time.Minute, "update.apply", &UpdateRequest{Version: version}, &out)
	return &out, err
}

func (c *Client) UpdateStatus(ctx context.Context) (*UpdateStatus, error) {
	var out UpdateStatus
	err := c.Call(ctx, "update.status", nil, &out)
	return &out, err
}
