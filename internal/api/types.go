package api

import "time"

// User represents an authenticated user
type User struct {
	ID                     int64      `json:"id"`
	Username               string     `json:"username"`
	IsAdmin                bool       `json:"is_admin"`
	IsSuspended            bool       `json:"is_suspended"`
	MemoryMB               int        `json:"memory_mb"`
	CPUPercent             int        `json:"cpu_percent"`
	MaxSites               int        `json:"max_sites"`  // -1 = unlimited
	StorageMB              int        `json:"storage_mb"` // -1 = unlimited
	SiteCount              int        `json:"site_count"` // Current number of sites
	StorageUsed            int64      `json:"storage_used"`
	IsImpersonating        bool       `json:"is_impersonating,omitempty"`
	ImpersonatedBy         string     `json:"impersonated_by,omitempty"`
	ImpersonationReason    string     `json:"impersonation_reason,omitempty"`
	ImpersonationExpiresAt *time.Time `json:"impersonation_expires_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
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

// ImpersonateRequest allows an admin to start a user impersonation session.
type ImpersonateRequest struct {
	TargetUsername string `json:"target_username"`
	AdminPassword  string `json:"admin_password"`
	Reason         string `json:"reason,omitempty"`
}

// ImpersonateResponse is returned after creating an impersonation session.
type ImpersonateResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      *User     `json:"user"`
	Message   string    `json:"message,omitempty"`
}

// CreateSiteRequest is the request body for creating a site
type CreateSiteRequest struct {
	Username   string `json:"-"` // Set from auth
	Domain     string `json:"domain"`
	Slug       string `json:"slug"`        // Optional, auto-generated from domain if empty
	SiteType   string `json:"site_type"`   // "php" or "wordpress"
	PHPVersion string `json:"php_version"` // 8.2/8.3/8.4/8.5
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
	SSLValid            bool         `json:"ssl_valid"`
	SSLStatus           string       `json:"ssl_status"`
	SSLReason           string       `json:"ssl_reason"`
	ForceHTTPS          bool         `json:"force_https"`
	CompressionEnabled  bool         `json:"compression_enabled"`
	GzipEnabled         bool         `json:"gzip_enabled"`
	ZstdEnabled         bool         `json:"zstd_enabled"`
	CacheControlEnabled bool         `json:"cache_control_enabled"`
	CacheControlValue   string       `json:"cache_control_value"`
	PHPVersion          string       `json:"php_version"`
	PHPMemoryLimit      string       `json:"php_memory_limit"`
	PHPPostMaxSize      string       `json:"php_post_max_size"`
	PHPUploadMaxSize    string       `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime int          `json:"php_max_execution_time"`
	PHPMaxInputVars     int          `json:"php_max_input_vars"`
	CreatedAt           time.Time    `json:"created_at"`
	Domains             []SiteDomain `json:"domains,omitempty"`
}

// UpdateSiteSettingsRequest is the request body for updating site runtime settings
type UpdateSiteSettingsRequest struct {
	Username            string `json:"-"` // Set from auth
	ForceHTTPS          bool   `json:"force_https"`
	CompressionEnabled  bool   `json:"compression_enabled"`
	GzipEnabled         bool   `json:"gzip_enabled"`
	ZstdEnabled         bool   `json:"zstd_enabled"`
	CacheControlEnabled bool   `json:"cache_control_enabled"`
	CacheControlValue   string `json:"cache_control_value"`
	PHPVersion          string `json:"php_version"`
	PHPMemoryLimit      string `json:"php_memory_limit"`
	PHPPostMaxSize      string `json:"php_post_max_size"`
	PHPUploadMaxSize    string `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime int    `json:"php_max_execution_time"`
	PHPMaxInputVars     int    `json:"php_max_input_vars"`
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

// UpdateUserResourcesRequest is the request body for updating user limits
type UpdateUserResourcesRequest struct {
	MemoryMB   int `json:"memory_mb"`
	CPUPercent int `json:"cpu_percent"`
	MaxSites   int `json:"max_sites"`  // -1 = unlimited
	StorageMB  int `json:"storage_mb"` // -1 = unlimited
}

// UpdateInfo contains information about available updates
type UpdateInfo struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseNotes    string    `json:"release_notes"`
	ReleaseURL      string    `json:"release_url"`
	PublishedAt     time.Time `json:"published_at"`
	Warning         string    `json:"warning,omitempty"`
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

// BackupCatalogSite is a selectable website target for backup policies.
type BackupCatalogSite struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// BackupCatalogDatabase is a selectable database target for backup policies.
type BackupCatalogDatabase struct {
	ID     string `json:"id"`
	DBName string `json:"db_name"`
}

// BackupCatalog contains available websites and databases for exclusions/restores.
type BackupCatalog struct {
	Sites     []BackupCatalogSite     `json:"sites"`
	Databases []BackupCatalogDatabase `json:"databases"`
}

// BackupConfig is the per-user backup repository + schedule + retention configuration.
type BackupConfig struct {
	Username           string         `json:"username"`
	Repository         string         `json:"repository"`
	HasPassword        bool           `json:"has_password"`
	BackendType        string         `json:"backend_type"`
	SFTPUsername       string         `json:"sftp_username,omitempty"`
	SFTPHost           string         `json:"sftp_host,omitempty"`
	SFTPPort           int            `json:"sftp_port,omitempty"`
	SFTPPath           string         `json:"sftp_path,omitempty"`
	S3Endpoint         string         `json:"s3_endpoint,omitempty"`
	S3Bucket           string         `json:"s3_bucket,omitempty"`
	S3Prefix           string         `json:"s3_prefix,omitempty"`
	S3Region           string         `json:"s3_region,omitempty"`
	S3BucketLookup     string         `json:"s3_bucket_lookup,omitempty"`
	S3ListObjectsV1    bool           `json:"s3_list_objects_v1,omitempty"`
	HasS3Credentials   bool           `json:"has_s3_credentials,omitempty"`
	HasS3SessionToken  bool           `json:"has_s3_session_token,omitempty"`
	Enabled            bool           `json:"enabled"`
	ScheduleCron       string         `json:"schedule_cron"`
	ExcludeSiteIDs     []string       `json:"exclude_site_ids"`
	ExcludeDatabaseIDs []string       `json:"exclude_database_ids"`
	KeepLast           int            `json:"keep_last"`
	KeepDaily          int            `json:"keep_daily"`
	KeepWeekly         int            `json:"keep_weekly"`
	KeepMonthly        int            `json:"keep_monthly"`
	LastStatus         string         `json:"last_status"`
	LastMessage        string         `json:"last_message"`
	RunningJobID       string         `json:"running_job_id,omitempty"`
	RunningStartedAt   *string        `json:"running_started_at,omitempty"`
	LastRunAt          *string        `json:"last_run_at,omitempty"`
	NextRunAt          *string        `json:"next_run_at,omitempty"`
	Catalog            *BackupCatalog `json:"catalog,omitempty"`
}

// SaveBackupConfigRequest updates the backup policy for a user.
type SaveBackupConfigRequest struct {
	Repository         string   `json:"repository"`
	RepositoryPassword string   `json:"repository_password,omitempty"` // Optional on update
	BackendType        string   `json:"backend_type"`
	SFTPUsername       string   `json:"sftp_username,omitempty"`
	SFTPHost           string   `json:"sftp_host,omitempty"`
	SFTPPort           int      `json:"sftp_port,omitempty"`
	SFTPPath           string   `json:"sftp_path,omitempty"`
	S3Endpoint         string   `json:"s3_endpoint,omitempty"`
	S3Bucket           string   `json:"s3_bucket,omitempty"`
	S3Prefix           string   `json:"s3_prefix,omitempty"`
	S3Region           string   `json:"s3_region,omitempty"`
	S3BucketLookup     string   `json:"s3_bucket_lookup,omitempty"`
	S3ListObjectsV1    bool     `json:"s3_list_objects_v1,omitempty"`
	S3AccessKeyID      string   `json:"s3_access_key_id,omitempty"`
	S3SecretAccessKey  string   `json:"s3_secret_access_key,omitempty"`
	S3SessionToken     string   `json:"s3_session_token,omitempty"`
	Enabled            bool     `json:"enabled"`
	ScheduleCron       string   `json:"schedule_cron"`
	ExcludeSiteIDs     []string `json:"exclude_site_ids"`
	ExcludeDatabaseIDs []string `json:"exclude_database_ids"`
	KeepLast           int      `json:"keep_last"`
	KeepDaily          int      `json:"keep_daily"`
	KeepWeekly         int      `json:"keep_weekly"`
	KeepMonthly        int      `json:"keep_monthly"`
}

// BackupConfigTestResponse reports backend connectivity validation.
type BackupConfigTestResponse struct {
	Status  string `json:"status"` // success | failed
	Message string `json:"message"`
}

// BackupRcloneStatus reports rclone availability/install lifecycle for backups.
type BackupRcloneStatus struct {
	Status     string `json:"status"` // available | missing | installing | failed
	Installed  bool   `json:"installed"`
	Version    string `json:"version,omitempty"`
	Message    string `json:"message,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// BackupRunResponse is returned when manual backup/restore jobs are triggered.
type BackupRunResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// BackupJob represents backup/restore execution history.
type BackupJob struct {
	ID         string  `json:"id"`
	Username   string  `json:"username"`
	JobType    string  `json:"job_type"`
	Status     string  `json:"status"`
	SnapshotID string  `json:"snapshot_id,omitempty"`
	Message    string  `json:"message,omitempty"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at,omitempty"`
}

// BackupSnapshot represents a restic snapshot (filtered per-user via tags).
type BackupSnapshotSummary struct {
	TotalSize uint64 `json:"total_size,omitempty"`
}

type BackupSnapshot struct {
	ID        string                 `json:"id"`
	ShortID   string                 `json:"short_id,omitempty"`
	Time      string                 `json:"time"`
	Hostname  string                 `json:"hostname,omitempty"`
	Tags      []string               `json:"tags,omitempty"`
	Paths     []string               `json:"paths,omitempty"`
	Summary   *BackupSnapshotSummary `json:"summary,omitempty"`
	TotalSize uint64                 `json:"total_size,omitempty"`
}

type DeleteSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

type RestoreSiteRequest struct {
	SnapshotID string `json:"snapshot_id"`
	SiteID     string `json:"site_id"`
}

type RestoreDatabaseRequest struct {
	SnapshotID string `json:"snapshot_id"`
	DatabaseID string `json:"database_id"`
}
