package api

import "time"

// User represents an authenticated user
type User struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	IsAdmin    bool      `json:"is_admin"`
	MemoryMB   int       `json:"memory_mb"`
	CPUPercent int       `json:"cpu_percent"`
	CreatedAt  time.Time `json:"created_at"`
}

// LoginRequest is the request body for login
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the response body for login
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      *User     `json:"user"`
}

// CreateSiteRequest is the request body for creating a site
type CreateSiteRequest struct {
	Username string `json:"-"` // Set from auth
	Domain   string `json:"domain"`
	SiteType string `json:"site_type"` // "php" or "wordpress"
}

// Site represents a website
type Site struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Domain       string    `json:"domain"`
	SiteType     string    `json:"site_type"`
	DocumentRoot string    `json:"document_root"`
	SSLEnabled   bool      `json:"ssl_enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateDatabaseRequest is the request body for creating a database
type CreateDatabaseRequest struct {
	Username string `json:"-"` // Set from auth
	Name     string `json:"name"`
}

// Database represents a MySQL database
type Database struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	DBName    string    `json:"db_name"`
	DBUser    string    `json:"db_user"`
	Password  string    `json:"password,omitempty"` // Only returned on create
	CreatedAt time.Time `json:"created_at"`
}

// AddSSHKeyRequest is the request body for adding an SSH key
type AddSSHKeyRequest struct {
	Username  string `json:"-"` // Set from auth
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// SSHKey represents an SSH public key
type SSHKey struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// SystemStatus represents the system status
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

// Service represents a system service
type Service struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "running", "stopped", "unknown"
	Enabled bool   `json:"enabled"`
}

// CreateUserRequest is the request body for creating a system user
type CreateUserRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	IsAdmin    bool   `json:"is_admin"`
	MemoryMB   int    `json:"memory_mb"`   // Memory limit in MB (0 = unlimited)
	CPUPercent int    `json:"cpu_percent"` // CPU limit as percentage (0 = unlimited, 100 = 1 core)
}

// UpdateUserRequest is the request body for updating a user
type UpdateUserRequest struct {
	Password string `json:"password,omitempty"`
	IsAdmin  bool   `json:"is_admin"`
}

// UpdateInfo contains information about available updates
type UpdateInfo struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseNotes    string    `json:"release_notes"`
	ReleaseURL      string    `json:"release_url"`
	PublishedAt     time.Time `json:"published_at"`
}

// PerformUpdateRequest is the request body for triggering an update
type PerformUpdateRequest struct {
	TargetVersion string `json:"target_version"`
}
