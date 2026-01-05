package models

import (
	"time"
)

// User represents a control panel user
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"` // admin, reseller, user
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Site represents a hosted website
type Site struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	Name        string            `json:"name"`
	Domain      string            `json:"domain"`
	Aliases     []string          `json:"aliases,omitempty"`
	PHPVersion  string            `json:"php_version"`
	RootPath    string            `json:"root_path"`
	PublicPath  string            `json:"public_path"` // relative to root_path
	AppType     string            `json:"app_type"`    // blank, wordpress
	DatabaseID  string            `json:"database_id,omitempty"`
	WorkerMode  bool              `json:"worker_mode"`
	WorkerFile  string            `json:"worker_file,omitempty"`
	WorkerNum   int               `json:"worker_num,omitempty"`
	SSL         bool              `json:"ssl"`
	Status      string            `json:"status"` // active, suspended, pending
	Environment map[string]string `json:"environment,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// PHPInstance represents a running FrankenPHP instance for a specific PHP version
type PHPInstance struct {
	Version     string    `json:"version"`
	Port        int       `json:"port"`
	AdminPort   int       `json:"admin_port"`
	BinaryPath  string    `json:"binary_path"`
	ConfigPath  string    `json:"config_path"`
	PIDFile     string    `json:"pid_file"`
	Status      string    `json:"status"` // running, stopped, error
	SiteCount   int       `json:"site_count"`
	ThreadCount int       `json:"thread_count"`
	MaxThreads  int       `json:"max_threads"`
	StartedAt   time.Time `json:"started_at,omitempty"`
}

// PHPVersionConfig holds configuration for a PHP version
type PHPVersionConfig struct {
	Version    string `json:"version"`
	Port       int    `json:"port"`
	AdminPort  int    `json:"admin_port"`
	BinaryPath string `json:"binary_path"`
	Enabled    bool   `json:"enabled"`
	NumThreads int    `json:"num_threads"`
	MaxThreads int    `json:"max_threads"`
}

// Config represents the main application configuration
type Config struct {
	AdminUser     string             `json:"admin_user"`
	AdminPassword string             `json:"admin_password"`
	AdminEmail    string             `json:"admin_email"` // Used for Let's Encrypt SSL
	JWTSecret     string             `json:"jwt_secret"`
	DataDir       string             `json:"data_dir"`
	SitesDir      string             `json:"sites_dir"`
	LogDir        string             `json:"log_dir"`
	ListenAddr    string             `json:"listen_addr"`
	PHPVersions   []PHPVersionConfig `json:"php_versions"`
	ProxyPort     int                `json:"proxy_port"`
	ProxySSLPort  int                `json:"proxy_ssl_port"`
}

// APIKey represents an API key for external integrations (WHMCS, etc.)
type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	Permissions []string  `json:"permissions"`
	UserID      string    `json:"user_id"`
	LastUsedAt  time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

// WHMCSProvisionRequest represents a WHMCS provisioning request
type WHMCSProvisionRequest struct {
	Action      string `json:"action"` // create, suspend, unsuspend, terminate
	ServiceID   string `json:"service_id"`
	Username    string `json:"username"`
	Domain      string `json:"domain"`
	Package     string `json:"package"`
	PHPVersion  string `json:"php_version"`
	DiskLimit   int64  `json:"disk_limit"`
	BWLimit     int64  `json:"bandwidth_limit"`
	CustomField map[string]string `json:"custom_fields,omitempty"`
}

// WHMCSResponse represents a response to WHMCS
type WHMCSResponse struct {
	Result  string `json:"result"` // success, error
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Stats represents system statistics
type Stats struct {
	TotalSites      int     `json:"total_sites"`
	ActiveSites     int     `json:"active_sites"`
	TotalUsers      int     `json:"total_users"`
	PHPInstances    int     `json:"php_instances"`
	DiskUsage       int64   `json:"disk_usage"`
	DiskTotal       int64   `json:"disk_total"`
	MemoryUsage     int64   `json:"memory_usage"`
	MemoryTotal     int64   `json:"memory_total"`
	CPUUsage        float64 `json:"cpu_usage"`
	Uptime          int64   `json:"uptime"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	ResourceID string   `json:"resource_id"`
	Details   string    `json:"details"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// UserLimits represents resource limits for a user
type UserLimits struct {
	Username      string `json:"username"`
	MaxSites      int    `json:"max_sites"`       // 0 = unlimited
	MaxRAMMB      int64  `json:"max_ram_mb"`      // 0 = unlimited, memory limit in MB
	MaxCPUPercent int    `json:"max_cpu_percent"` // 0 = unlimited, CPU limit (100 = 1 core)
	MaxProcesses  int    `json:"max_processes"`   // 0 = unlimited, max concurrent processes
}

// Database represents a MySQL database
type Database struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	SiteID    string    `json:"site_id,omitempty"` // Optional: linked site
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Password  string    `json:"password,omitempty"` // Only returned on create
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DatabaseServerStatus represents the MySQL server status
type DatabaseServerStatus struct {
	Installed     bool   `json:"installed"`
	Running       bool   `json:"running"`
	Version       string `json:"version,omitempty"`
	DatabaseCount int    `json:"database_count"`
}

