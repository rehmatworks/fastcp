package agent

import (
	"encoding/json"
	"time"
)

// Request is one agent RPC call. Each connection carries exactly one call.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a Request with the same ID.
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Code   string          `json:"code,omitempty"`
}

// Error codes returned in Response.Code.
const (
	CodeInvalid  = "invalid"
	CodeConflict = "conflict"
	CodeNotFound = "not_found"
	CodeDenied   = "denied"
	CodeInternal = "internal"
)

// ---------------------------------------------------------------------------
// Desired state (config.apply)

// State is the complete desired hosting configuration. The panel pushes it
// after every change and at startup; the agent renders Caddy, PHP-FPM, cron
// and resource limits from it and keeps a copy for restarts.
type State struct {
	Users []StateUser `json:"users"`
	Sites []StateSite `json:"sites"`
}

type StateUser struct {
	Username   string    `json:"username"`
	Suspended  bool      `json:"suspended"`
	MemoryMB   int       `json:"memory_mb"`   // -1 or 0 = unlimited
	CPUPercent int       `json:"cpu_percent"` // -1 or 0 = unlimited
	Cron       []CronJob `json:"cron"`
}

type CronJob struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	Enabled    bool   `json:"enabled"`
}

type StateSite struct {
	ID                  string        `json:"id"`
	Username            string        `json:"username"`
	Slug                string        `json:"slug"`
	PHPVersion          string        `json:"php_version"`
	ProxyURL            string        `json:"proxy_url,omitempty"` // set for reverse proxy sites, which run no PHP
	Domains             []StateDomain `json:"domains"`
	ForceHTTPS          bool          `json:"force_https"`
	CompressionEnabled  bool          `json:"compression_enabled"`
	GzipEnabled         bool          `json:"gzip_enabled"`
	ZstdEnabled         bool          `json:"zstd_enabled"`
	CacheControl        string        `json:"cache_control"` // empty = no header
	PHPMemoryLimit      string        `json:"php_memory_limit"`
	PHPPostMaxSize      string        `json:"php_post_max_size"`
	PHPUploadMaxSize    string        `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime int           `json:"php_max_execution_time"`
	PHPMaxInputVars     int           `json:"php_max_input_vars"`
}

type StateDomain struct {
	Domain            string `json:"domain"`
	Primary           bool   `json:"primary"`
	RedirectToPrimary bool   `json:"redirect_to_primary"`
}

type ApplyResult struct {
	Warnings []string `json:"warnings,omitempty"`
}

// ---------------------------------------------------------------------------
// Auth and users

type AuthRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type DeleteUserRequest struct {
	Username  string           `json:"username"`
	Databases []DatabaseTarget `json:"databases"`
}

type DatabaseTarget struct {
	DBName string `json:"db_name"`
	DBUser string `json:"db_user"`
}

type UserExecRequest struct {
	Username   string            `json:"username"`
	Script     string            `json:"script"`
	Env        map[string]string `json:"env,omitempty"`
	Stdin      string            `json:"stdin,omitempty"`
	TimeoutSec int               `json:"timeout_sec,omitempty"`
	MaxOutput  int               `json:"max_output,omitempty"`
}

type UserExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

// ---------------------------------------------------------------------------
// Sites

type CreateSiteRequest struct {
	Username string `json:"username"`
	Slug     string `json:"slug"`
	Domain   string `json:"domain"`
}

type DeleteSiteRequest struct {
	Username string `json:"username"`
	Slug     string `json:"slug"`
}

// Site log kinds for SiteLogRequest.
const (
	SiteLogPHP    = "php"
	SiteLogAccess = "access"
)

type SiteLogRequest struct {
	Username string `json:"username"`
	Slug     string `json:"slug"`
	Kind     string `json:"kind"`
}

// SiteLogResult holds the end of a site's log file. Data starts at a line
// boundary; Truncated reports that earlier lines were left out.
type SiteLogResult struct {
	Exists    bool      `json:"exists"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	Data      string    `json:"data"`
	Truncated bool      `json:"truncated"`
}

type InstallWordPressRequest struct {
	Username string `json:"username"`
	Slug     string `json:"slug"`
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	DBPass   string `json:"db_pass"`
}

// ---------------------------------------------------------------------------
// Databases

type CreateDatabaseRequest struct {
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	Password string `json:"password"`
}

type ResetDatabasePasswordRequest struct {
	DBUser   string `json:"db_user"`
	Password string `json:"password"`
}

type MySQLConfig struct {
	BufferPoolMB   int  `json:"buffer_pool_mb"`
	MaxConnections int  `json:"max_connections"`
	PerfSchema     bool `json:"perf_schema"`
	DetectedRAMMB  int  `json:"detected_ram_mb,omitempty"`
}

// ---------------------------------------------------------------------------
// SSH

type AddSSHKeyRequest struct {
	Username  string `json:"username"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type RemoveSSHKeyRequest struct {
	Username string `json:"username"`
	KeyID    string `json:"key_id"`
}

type SSHConfig struct {
	Port         int  `json:"port"`
	PasswordAuth bool `json:"password_auth"`
	// Read-only: whether any account has an authorized_keys entry.
	KeysInstalled bool `json:"keys_installed"`
}

// ---------------------------------------------------------------------------
// System

type SystemStatus struct {
	Hostname     string     `json:"hostname"`
	OS           string     `json:"os"`
	Kernel       string     `json:"kernel"`
	Architecture string     `json:"architecture"`
	Uptime       int64      `json:"uptime"`
	LoadAverage  [3]float64 `json:"load_average"`
	CPUCount     int        `json:"cpu_count"`
	MemoryTotal  uint64     `json:"memory_total"`
	MemoryUsed   uint64     `json:"memory_used"`
	SwapTotal    uint64     `json:"swap_total"`
	SwapUsed     uint64     `json:"swap_used"`
	DiskTotal    uint64     `json:"disk_total"`
	DiskUsed     uint64     `json:"disk_used"`
	MySQLVersion string     `json:"mysql_version"`
	CaddyVersion string     `json:"caddy_version"`
	PHPVersions  []string   `json:"php_versions"`
	AgentVersion string     `json:"agent_version"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
	State       string `json:"state"`
	Enabled     bool   `json:"enabled"`
}

// CaddyConfig holds tunable web server settings.
type CaddyConfig struct {
	Profile       string `json:"profile"` // low_ram | balanced | high_throughput | custom
	AccessLogs    bool   `json:"access_logs"`
	ReadHeader    string `json:"read_header"`
	ReadBody      string `json:"read_body"`
	WriteTimeout  string `json:"write_timeout"`
	IdleTimeout   string `json:"idle_timeout"`
	GracePeriod   string `json:"grace_period"`
	MaxHeaderSize int    `json:"max_header_size"`
}

type FirewallRule struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Action    string `json:"action"`
	From      string `json:"from"`
	IPVersion string `json:"ip_version"`
	Protected bool   `json:"protected"`
	Raw       string `json:"raw"`
}

type FirewallStatus struct {
	Installed      bool           `json:"installed"`
	Enabled        bool           `json:"enabled"`
	Available      bool           `json:"available"`
	Message        string         `json:"message,omitempty"`
	SSHPorts       []int          `json:"ssh_ports"`
	ProtectedPorts []int          `json:"protected_ports"`
	Rules          []FirewallRule `json:"rules"`
}

type FirewallRuleRequest struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`   // tcp | udp
	IPVersion string `json:"ip_version"` // both | ipv4 | ipv6
}

type ToolStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

// ---------------------------------------------------------------------------
// PHP lifecycle

type PHPVersionInfo struct {
	Version      string `json:"version"`
	Installed    bool   `json:"installed"`
	Available    bool   `json:"available"`
	PatchVersion string `json:"patch_version,omitempty"`
	Candidate    string `json:"candidate,omitempty"`
	EOLDate      string `json:"eol_date,omitempty"`
	Status       string `json:"status"` // active | security | eol | unknown
	Default      bool   `json:"default"`
	Sites        int    `json:"sites"`
	FPMRunning   bool   `json:"fpm_running"`
}

type PHPSettings struct {
	DefaultVersion      string `json:"default_version"`
	AutoPatch           bool   `json:"auto_patch"`
	AutoInstall         bool   `json:"auto_install"`
	FollowLatestDefault bool   `json:"follow_latest_default"`
	MaintenanceHour     int    `json:"maintenance_hour"`   // 0-23, server local time
	MaintenanceMinute   int    `json:"maintenance_minute"` // 0-59
}

type PHPStatus struct {
	Versions        []PHPVersionInfo `json:"versions"`
	Settings        PHPSettings      `json:"settings"`
	Repository      string           `json:"repository"`
	RepositoryError string           `json:"repository_error,omitempty"`
	LastMaintenance *Job             `json:"last_maintenance,omitempty"`
	NextMaintenance string           `json:"next_maintenance,omitempty"`
	Jobs            []Job            `json:"jobs"`
}

type PHPVersionRequest struct {
	Version string `json:"version"`
}

// Job is a long-running agent operation whose status survives restarts.
type Job struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status"` // queued | running | succeeded | failed
	Message    string `json:"message,omitempty"`
	Log        string `json:"log,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type JobRequest struct {
	ID string `json:"id"`
}

// ---------------------------------------------------------------------------
// Updates

type UpdateRequest struct {
	Version string `json:"version"`
}

type UpdateStatus struct {
	CurrentVersion string `json:"current_version"`
	Job            *Job   `json:"job,omitempty"`
}

// ---------------------------------------------------------------------------
// Backups

type SpoolRequest struct {
	Username string `json:"username"`
}

type SpoolResult struct {
	Dir string `json:"dir"`
}
