package agent

// Request represents an agent RPC request
type Request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// Response represents an agent RPC response
type Response struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CreateSiteDirectoryRequest is the request for creating a site directory
type CreateSiteDirectoryRequest struct {
	Username string `json:"username"`
	Domain   string `json:"domain"`
	Slug     string `json:"slug"` // Directory name for the site
	SiteType string `json:"site_type"`
}

// DeleteSiteDirectoryRequest is the request for deleting a site directory
type DeleteSiteDirectoryRequest struct {
	Username string `json:"username"`
	Slug     string `json:"slug"` // Directory name for the site
}

// InstallWordPressRequest is the request for installing WordPress
type InstallWordPressRequest struct {
	Username string `json:"username"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	DBPass   string `json:"db_pass"`
}

// InstallWordPressResponse is the response for installing WordPress
type InstallWordPressResponse struct {
	Status string `json:"status"`
	DBName string `json:"db_name"`
	DBUser string `json:"db_user"`
	DBPass string `json:"db_pass"`
}

// CreateDatabaseRequest is the request for creating a MySQL database
type CreateDatabaseRequest struct {
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	Password string `json:"password"`
}

// DeleteDatabaseRequest is the request for deleting a MySQL database
type DeleteDatabaseRequest struct {
	DBName string `json:"db_name"`
	DBUser string `json:"db_user"`
}

// ResetDatabasePasswordRequest is the request for rotating a MySQL user's password
type ResetDatabasePasswordRequest struct {
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	Password string `json:"password"`
}

// AddSSHKeyRequest is the request for adding an SSH key
type AddSSHKeyRequest struct {
	Username  string `json:"username"`
	KeyID     string `json:"key_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// RemoveSSHKeyRequest is the request for removing an SSH key
type RemoveSSHKeyRequest struct {
	Username  string `json:"username"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

// SystemStatus represents system status information
type SystemStatus struct {
	Hostname             string   `json:"hostname"`
	OS                   string   `json:"os"`
	Uptime               int64    `json:"uptime"`
	LoadAverage          float64  `json:"load_average"`
	MemoryTotal          uint64   `json:"memory_total"`
	MemoryUsed           uint64   `json:"memory_used"`
	DiskTotal            uint64   `json:"disk_total"`
	DiskUsed             uint64   `json:"disk_used"`
	PHPVersion           string   `json:"php_version"`
	MySQLVersion         string   `json:"mysql_version"`
	CaddyVersion         string   `json:"caddy_version"`
	PHPAvailableVersions []string `json:"php_available_versions"`
	KernelVersion        string   `json:"kernel_version"`
	Architecture         string   `json:"architecture"`
	TotalUsers           int      `json:"total_users"`
	TotalWebsites        int      `json:"total_websites"`
	TotalDatabases       int      `json:"total_databases"`
}

// ServiceStatus represents a service status
type ServiceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}

// MySQLConfig represents tunable MySQL settings
type MySQLConfig struct {
	BufferPoolMB   int  `json:"buffer_pool_mb"`
	MaxConnections int  `json:"max_connections"`
	PerfSchema     bool `json:"perf_schema"`
	DetectedRAMMB  int  `json:"detected_ram_mb"`
}

// SSHConfig represents tunable SSH daemon settings
type SSHConfig struct {
	Port         int  `json:"port"`
	PasswordAuth bool `json:"password_auth"`
}

// PHPDefaultConfig represents the system default PHP version for new sites
type PHPDefaultConfig struct {
	DefaultPHPVersion    string   `json:"default_php_version"`
	AvailablePHPVersions []string `json:"available_php_versions,omitempty"`
}

// PHPVersionInstallRequest requests installation of a specific PHP runtime.
type PHPVersionInstallRequest struct {
	Version string `json:"version"`
}

// CaddyConfig represents tunable Caddy performance/logging settings
type CaddyConfig struct {
	Profile       string `json:"profile"` // balanced | low_ram | high_throughput
	AccessLogs    bool   `json:"access_logs"`
	ExpertMode    bool   `json:"expert_mode"`
	ReadHeader    string `json:"read_header"`
	ReadBody      string `json:"read_body"`
	WriteTimeout  string `json:"write_timeout"`
	IdleTimeout   string `json:"idle_timeout"`
	GracePeriod   string `json:"grace_period"`
	MaxHeaderSize int    `json:"max_header_size"`
}

// FirewallRule represents a UFW rule entry
type FirewallRule struct {
	Rule        string `json:"rule"`        // e.g. 22/tcp
	Action      string `json:"action"`      // ALLOW or DENY
	From        string `json:"from"`        // e.g. Anywhere
	IPVersion   string `json:"ip_version"`  // ipv4 or ipv6
	Description string `json:"description"` // Optional display text
}

// FirewallStatus represents UFW status and current rules
type FirewallStatus struct {
	Installed        bool           `json:"installed"`
	Enabled          bool           `json:"enabled"`
	ControlPanelPort int            `json:"control_panel_port"`
	Rules            []FirewallRule `json:"rules"`
}

// FirewallRuleRequest requests allow/deny/delete operations for a port
type FirewallRuleRequest struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`   // tcp or udp
	IPVersion string `json:"ip_version"` // both, ipv4, ipv6
}

// RcloneStatus represents rclone availability on the server.
type RcloneStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}

// CreateUserRequest is the request for creating a system user
type CreateUserRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	MemoryMB   int    `json:"memory_mb"`   // Memory limit in MB (0 = unlimited)
	CPUPercent int    `json:"cpu_percent"` // CPU limit as percentage (0 = unlimited, 100 = 1 core)
}

// DeleteUserRequest is the request for deleting a system user
type DeleteUserRequest struct {
	Username string `json:"username"`
}

// UpdateUserLimitsRequest is the request for updating system user resource limits
type UpdateUserLimitsRequest struct {
	Username   string `json:"username"`
	MemoryMB   int    `json:"memory_mb"`
	CPUPercent int    `json:"cpu_percent"`
}

// PerformUpdateRequest is the request for updating FastCP
type PerformUpdateRequest struct {
	TargetVersion string `json:"target_version"`
}

// SyncCronJobsRequest is the request for syncing cron jobs for a user
type SyncCronJobsRequest struct {
	Username string    `json:"username"`
	Jobs     []CronJob `json:"jobs"`
}

// CronJob represents a cron job entry
type CronJob struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	Enabled    bool   `json:"enabled"`
}
