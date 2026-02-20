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
	SiteType string `json:"site_type"`
}

// DeleteSiteDirectoryRequest is the request for deleting a site directory
type DeleteSiteDirectoryRequest struct {
	Username string `json:"username"`
	Domain   string `json:"domain"`
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
	Hostname     string  `json:"hostname"`
	OS           string  `json:"os"`
	Uptime       int64   `json:"uptime"`
	LoadAverage  float64 `json:"load_average"`
	MemoryTotal  uint64  `json:"memory_total"`
	MemoryUsed   uint64  `json:"memory_used"`
	DiskTotal    uint64  `json:"disk_total"`
	DiskUsed     uint64  `json:"disk_used"`
	PHPVersion   string  `json:"php_version"`
	MySQLVersion string  `json:"mysql_version"`
}

// ServiceStatus represents a service status
type ServiceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
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
