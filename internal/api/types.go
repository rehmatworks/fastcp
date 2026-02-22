package api

import "time"

// User represents an authenticated user
type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	IsAdmin     bool      `json:"is_admin"`
	IsSuspended bool      `json:"is_suspended"`
	MemoryMB    int       `json:"memory_mb"`
	CPUPercent  int       `json:"cpu_percent"`
	MaxSites    int       `json:"max_sites"`  // -1 = unlimited
	StorageMB   int       `json:"storage_mb"` // -1 = unlimited
	SiteCount   int       `json:"site_count"` // Current number of sites
	StorageUsed int64     `json:"storage_used"`
	CreatedAt   time.Time `json:"created_at"`
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
	Slug     string `json:"slug"` // Optional, auto-generated from domain if empty
	SiteType string `json:"site_type"` // "php" or "wordpress"
}

// Site represents a website
type Site struct {
	ID                  string       `json:"id"`
	Username            string       `json:"username"`
	Domain              string       `json:"domain"`
	Slug                string       `json:"slug"`
	SiteType            string       `json:"site_type"`
	DocumentRoot        string       `json:"document_root"`
	SSLEnabled          bool         `json:"ssl_enabled"`
	CompressionEnabled  bool         `json:"compression_enabled"`
	GzipEnabled         bool         `json:"gzip_enabled"`
	ZstdEnabled         bool         `json:"zstd_enabled"`
	CacheControlEnabled bool         `json:"cache_control_enabled"`
	CacheControlValue   string       `json:"cache_control_value"`
	CreatedAt           time.Time    `json:"created_at"`
	Domains             []SiteDomain `json:"domains,omitempty"`
}

// UpdateSiteSettingsRequest is the request body for updating site runtime settings
type UpdateSiteSettingsRequest struct {
	Username            string `json:"-"` // Set from auth
	CompressionEnabled  bool   `json:"compression_enabled"`
	GzipEnabled         bool   `json:"gzip_enabled"`
	ZstdEnabled         bool   `json:"zstd_enabled"`
	CacheControlEnabled bool   `json:"cache_control_enabled"`
	CacheControlValue   string `json:"cache_control_value"`
}

// ValidateSlugRequest is the request body for validating a site slug
type ValidateSlugRequest struct {
	Username string `json:"-"` // Set from auth
	Slug     string `json:"slug"`
}

// SiteDomain represents a domain attached to a site
type SiteDomain struct {
	ID                int64     `json:"id"`
	SiteID            string    `json:"site_id"`
	Domain            string    `json:"domain"`
	IsPrimary         bool      `json:"is_primary"`
	RedirectToPrimary bool      `json:"redirect_to_primary"`
	CreatedAt         time.Time `json:"created_at"`
}

// AddDomainRequest is the request body for adding a domain to a site
type AddDomainRequest struct {
	Username          string `json:"-"` // Set from auth
	SiteID            string `json:"site_id"`
	Domain            string `json:"domain"`
	RedirectToPrimary bool   `json:"redirect_to_primary"`
}

// UpdateDomainRequest is the request body for updating a domain
type UpdateDomainRequest struct {
	Username          string `json:"-"` // Set from auth
	DomainID          int64  `json:"domain_id"`
	RedirectToPrimary bool   `json:"redirect_to_primary"`
}

// SetPrimaryDomainRequest is the request body for setting a primary domain
type SetPrimaryDomainRequest struct {
	Username string `json:"-"` // Set from auth
	DomainID int64  `json:"domain_id"`
}

// DeleteDomainRequest is the request body for deleting a domain
type DeleteDomainRequest struct {
	Username string `json:"-"` // Set from auth
	DomainID int64  `json:"domain_id"`
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
	MaxSites   int    `json:"max_sites"`   // Max websites (-1 = unlimited)
	StorageMB  int    `json:"storage_mb"`  // Max storage in MB (-1 = unlimited)
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

// CronJob represents a scheduled cron job
type CronJob struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	Expression  string    `json:"expression"`
	Command     string    `json:"command"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description,omitempty"` // Human-readable schedule description
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateCronJobRequest is the request body for creating a cron job
type CreateCronJobRequest struct {
	Username   string `json:"-"` // Set from auth
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
}

// UpdateCronJobRequest is the request body for updating a cron job
type UpdateCronJobRequest struct {
	Username   string `json:"-"` // Set from auth
	ID         string `json:"id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	Enabled    bool   `json:"enabled"`
}
