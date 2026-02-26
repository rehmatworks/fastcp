package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite database
type DB struct {
	*sql.DB
}

// Open opens the SQLite database and runs migrations
func Open(path string) (*DB, error) {
	// SQLite connection string with performance optimizations:
	// - _journal_mode=WAL: Write-Ahead Logging for concurrent reads + faster writes
	// - _synchronous=NORMAL: Safe with WAL, much faster than FULL
	// - _foreign_keys=on: Enforce foreign key constraints
	// - _busy_timeout=5000: Wait up to 5s for locks instead of failing immediately
	// - _cache_size=-64000: 64MB page cache (negative = KB)
	// - _temp_store=MEMORY: Store temp tables in memory
	connStr := path + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on&_busy_timeout=5000&_cache_size=-64000&_temp_store=MEMORY"

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Apply additional PRAGMA settings that can't be set via connection string
	pragmas := []string{
		"PRAGMA mmap_size = 268435456", // 256MB memory-mapped I/O
		"PRAGMA page_size = 4096",      // Optimal page size (may not take effect on existing DB)
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			// Non-fatal: some pragmas may not apply to existing databases
		}
	}

	// Run migrations
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			is_admin INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE NOT NULL,
			username TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS impersonation_sessions (
			token TEXT PRIMARY KEY,
			admin_username TEXT NOT NULL,
			target_username TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (token) REFERENCES sessions(token) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS impersonation_audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			admin_username TEXT NOT NULL,
			target_username TEXT NOT NULL,
			action TEXT NOT NULL,
			token TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS sites (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			domain TEXT UNIQUE NOT NULL,
			slug TEXT NOT NULL,
			site_type TEXT NOT NULL DEFAULT 'php',
			document_root TEXT NOT NULL,
			ssl_enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS site_domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_id TEXT NOT NULL,
			domain TEXT UNIQUE NOT NULL,
			is_primary INTEGER DEFAULT 0,
			redirect_to_primary INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
		)`,

		// Migration: add redirect_to_primary column if missing
		`ALTER TABLE site_domains ADD COLUMN redirect_to_primary INTEGER DEFAULT 0`,

		`CREATE TABLE IF NOT EXISTS databases (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			db_name TEXT UNIQUE NOT NULL,
			db_user TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS ssh_keys (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			name TEXT NOT NULL,
			public_key TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,

		`CREATE INDEX IF NOT EXISTS idx_sites_username ON sites(username)`,
		`CREATE INDEX IF NOT EXISTS idx_databases_username ON databases(username)`,
		`CREATE INDEX IF NOT EXISTS idx_ssh_keys_username ON ssh_keys(username)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_impersonation_sessions_expires_at ON impersonation_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_impersonation_audit_admin_created ON impersonation_audit_logs(admin_username, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_impersonation_audit_target_created ON impersonation_audit_logs(target_username, created_at DESC)`,

		// Add resource limits to users table (safe to run multiple times - will fail silently if exists)
		`ALTER TABLE users ADD COLUMN memory_mb INTEGER DEFAULT 512`,
		`ALTER TABLE users ADD COLUMN cpu_percent INTEGER DEFAULT 100`,
		`ALTER TABLE users ADD COLUMN max_sites INTEGER DEFAULT -1`,
		`ALTER TABLE users ADD COLUMN storage_mb INTEGER DEFAULT -1`,

		// Cron jobs table
		`CREATE TABLE IF NOT EXISTS cron_jobs (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			name TEXT NOT NULL,
			expression TEXT NOT NULL,
			command TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_jobs_username ON cron_jobs(username)`,

		// Add slug column to sites table (migration for existing databases)
		`ALTER TABLE sites ADD COLUMN slug TEXT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_username_slug ON sites(username, slug)`,

		// Add is_suspended column to users table
		`ALTER TABLE users ADD COLUMN is_suspended INTEGER DEFAULT 0`,

		// Add db_password column to databases table (encrypted)
		`ALTER TABLE databases ADD COLUMN db_password TEXT DEFAULT ''`,

		// Per-site web settings
		`ALTER TABLE sites ADD COLUMN compression_enabled INTEGER DEFAULT 1`,
		`ALTER TABLE sites ADD COLUMN gzip_enabled INTEGER DEFAULT 1`,
		`ALTER TABLE sites ADD COLUMN zstd_enabled INTEGER DEFAULT 1`,
		`ALTER TABLE sites ADD COLUMN cache_control_enabled INTEGER DEFAULT 0`,
		`ALTER TABLE sites ADD COLUMN cache_control_value TEXT DEFAULT ''`,
		`ALTER TABLE sites ADD COLUMN php_version TEXT DEFAULT '8.4'`,
		`ALTER TABLE sites ADD COLUMN force_https INTEGER DEFAULT 1`,
		`ALTER TABLE sites ADD COLUMN php_memory_limit TEXT DEFAULT '256M'`,
		`ALTER TABLE sites ADD COLUMN php_post_max_size TEXT DEFAULT '64M'`,
		`ALTER TABLE sites ADD COLUMN php_upload_max_filesize TEXT DEFAULT '64M'`,
		`ALTER TABLE sites ADD COLUMN php_max_execution_time INTEGER DEFAULT 300`,
		`ALTER TABLE sites ADD COLUMN php_max_input_vars INTEGER DEFAULT 5000`,

		// Backup configuration (one repository/policy per user)
		`CREATE TABLE IF NOT EXISTS backup_configs (
			username TEXT PRIMARY KEY,
			repository TEXT NOT NULL DEFAULT '',
			password_enc TEXT NOT NULL DEFAULT '',
			backend_type TEXT NOT NULL DEFAULT 'sftp',
			sftp_username TEXT NOT NULL DEFAULT '',
			sftp_host TEXT NOT NULL DEFAULT '',
			sftp_port INTEGER NOT NULL DEFAULT 22,
			sftp_path TEXT NOT NULL DEFAULT '',
			s3_endpoint TEXT NOT NULL DEFAULT '',
			s3_bucket TEXT NOT NULL DEFAULT '',
			s3_prefix TEXT NOT NULL DEFAULT '',
			s3_region TEXT NOT NULL DEFAULT '',
			s3_bucket_lookup TEXT NOT NULL DEFAULT 'auto',
			s3_list_objects_v1 INTEGER NOT NULL DEFAULT 0,
			s3_access_key_enc TEXT NOT NULL DEFAULT '',
			s3_secret_key_enc TEXT NOT NULL DEFAULT '',
			s3_session_token_enc TEXT NOT NULL DEFAULT '',
			enabled INTEGER DEFAULT 0,
			schedule_cron TEXT NOT NULL DEFAULT '0 2 * * *',
			exclude_site_ids TEXT NOT NULL DEFAULT '[]',
			exclude_database_ids TEXT NOT NULL DEFAULT '[]',
			keep_last INTEGER NOT NULL DEFAULT 7,
			keep_daily INTEGER NOT NULL DEFAULT 7,
			keep_weekly INTEGER NOT NULL DEFAULT 4,
			keep_monthly INTEGER NOT NULL DEFAULT 6,
			last_run_at DATETIME,
			next_run_at DATETIME,
			last_status TEXT NOT NULL DEFAULT 'idle',
			last_message TEXT NOT NULL DEFAULT '',
			running_job_id TEXT NOT NULL DEFAULT '',
			running_started_at DATETIME,
			last_prune_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_configs_enabled_next_run ON backup_configs(enabled, next_run_at)`,
		`ALTER TABLE backup_configs ADD COLUMN last_prune_at DATETIME`,
		`ALTER TABLE backup_configs ADD COLUMN backend_type TEXT NOT NULL DEFAULT 'sftp'`,
		`ALTER TABLE backup_configs ADD COLUMN sftp_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN sftp_host TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN sftp_port INTEGER NOT NULL DEFAULT 22`,
		`ALTER TABLE backup_configs ADD COLUMN sftp_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_endpoint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_bucket TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_prefix TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_region TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_bucket_lookup TEXT NOT NULL DEFAULT 'auto'`,
		`ALTER TABLE backup_configs ADD COLUMN s3_list_objects_v1 INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE backup_configs ADD COLUMN s3_access_key_enc TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_secret_key_enc TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_configs ADD COLUMN s3_session_token_enc TEXT NOT NULL DEFAULT ''`,

		// Backup + restore job history
		`CREATE TABLE IF NOT EXISTS backup_jobs (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			job_type TEXT NOT NULL,
			status TEXT NOT NULL,
			snapshot_id TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_jobs_username_started ON backup_jobs(username, started_at DESC)`,
	}

	for _, m := range migrations {
		_, err := db.Exec(m)
		if err != nil {
			// Ignore errors for ALTER TABLE (column may already exist)
			if strings.Contains(m, "ALTER TABLE") {
				continue
			}
			return fmt.Errorf("migration failed: %s: %w", m[:min(50, len(m))], err)
		}
	}

	return nil
}

// User represents a system user
type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	IsAdmin     bool      `json:"is_admin"`
	IsSuspended bool      `json:"is_suspended"`
	MemoryMB    int       `json:"memory_mb"`
	CPUPercent  int       `json:"cpu_percent"`
	MaxSites    int       `json:"max_sites"`  // -1 = unlimited
	StorageMB   int       `json:"storage_mb"` // -1 = unlimited
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Site represents a website
type Site struct {
	ID                  string        `json:"id"`
	Username            string        `json:"username"`
	Domain              string        `json:"domain"`
	Slug                string        `json:"slug"`
	SiteType            string        `json:"site_type"`
	DocumentRoot        string        `json:"document_root"`
	SSLEnabled          bool          `json:"ssl_enabled"`
	ForceHTTPS          bool          `json:"force_https"`
	CompressionEnabled  bool          `json:"compression_enabled"`
	GzipEnabled         bool          `json:"gzip_enabled"`
	ZstdEnabled         bool          `json:"zstd_enabled"`
	CacheControlEnabled bool          `json:"cache_control_enabled"`
	CacheControlValue   string        `json:"cache_control_value"`
	PHPVersion          string        `json:"php_version"`
	PHPMemoryLimit      string        `json:"php_memory_limit"`
	PHPPostMaxSize      string        `json:"php_post_max_size"`
	PHPUploadMaxSize    string        `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime int           `json:"php_max_execution_time"`
	PHPMaxInputVars     int           `json:"php_max_input_vars"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	Domains             []*SiteDomain `json:"domains,omitempty"`
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

// Database represents a MySQL database
type Database struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	DBName     string    `json:"db_name"`
	DBUser     string    `json:"db_user"`
	DBPassword string    `json:"db_password,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SSHKey represents an SSH public key
type SSHKey struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// Session represents a login session
type Session struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ImpersonationSession tracks admin-issued impersonation tokens.
type ImpersonationSession struct {
	Token          string    `json:"token"`
	AdminUsername  string    `json:"admin_username"`
	TargetUsername string    `json:"target_username"`
	Reason         string    `json:"reason"`
	IPAddress      string    `json:"ip_address"`
	UserAgent      string    `json:"user_agent"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// ImpersonationAuditLog tracks start/stop/failure events for impersonation.
type ImpersonationAuditLog struct {
	ID             int64     `json:"id"`
	AdminUsername  string    `json:"admin_username"`
	TargetUsername string    `json:"target_username"`
	Action         string    `json:"action"`
	Token          string    `json:"token"`
	Reason         string    `json:"reason"`
	IPAddress      string    `json:"ip_address"`
	UserAgent      string    `json:"user_agent"`
	CreatedAt      time.Time `json:"created_at"`
}

// CronJob represents a scheduled cron job
type CronJob struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Name       string    `json:"name"`
	Expression string    `json:"expression"`
	Command    string    `json:"command"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// BackupConfig stores per-user backup repository and policy settings.
type BackupConfig struct {
	Username           string     `json:"username"`
	Repository         string     `json:"repository"`
	PasswordEnc        string     `json:"-"`
	BackendType        string     `json:"backend_type"`
	SFTPUsername       string     `json:"sftp_username"`
	SFTPHost           string     `json:"sftp_host"`
	SFTPPort           int        `json:"sftp_port"`
	SFTPPath           string     `json:"sftp_path"`
	S3Endpoint         string     `json:"s3_endpoint"`
	S3Bucket           string     `json:"s3_bucket"`
	S3Prefix           string     `json:"s3_prefix"`
	S3Region           string     `json:"s3_region"`
	S3BucketLookup     string     `json:"s3_bucket_lookup"`
	S3ListObjectsV1    bool       `json:"s3_list_objects_v1"`
	S3AccessKeyEnc     string     `json:"-"`
	S3SecretKeyEnc     string     `json:"-"`
	S3SessionTokenEnc  string     `json:"-"`
	Enabled            bool       `json:"enabled"`
	ScheduleCron       string     `json:"schedule_cron"`
	ExcludeSiteIDs     string     `json:"exclude_site_ids"`
	ExcludeDatabaseIDs string     `json:"exclude_database_ids"`
	KeepLast           int        `json:"keep_last"`
	KeepDaily          int        `json:"keep_daily"`
	KeepWeekly         int        `json:"keep_weekly"`
	KeepMonthly        int        `json:"keep_monthly"`
	LastRunAt          *time.Time `json:"last_run_at,omitempty"`
	NextRunAt          *time.Time `json:"next_run_at,omitempty"`
	LastStatus         string     `json:"last_status"`
	LastMessage        string     `json:"last_message"`
	RunningJobID       string     `json:"running_job_id"`
	RunningStartedAt   *time.Time `json:"running_started_at,omitempty"`
	LastPruneAt        *time.Time `json:"last_prune_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// BackupJob stores historical execution status for backup/restore operations.
type BackupJob struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	JobType    string     `json:"job_type"`
	Status     string     `json:"status"`
	SnapshotID string     `json:"snapshot_id,omitempty"`
	Message    string     `json:"message,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// User operations
func (db *DB) GetUser(ctx context.Context, username string) (*User, error) {
	var u User
	err := db.QueryRowContext(ctx,
		`SELECT id, username, is_admin, COALESCE(is_suspended, 0), COALESCE(memory_mb, 512), COALESCE(cpu_percent, 100), 
		 COALESCE(max_sites, -1), COALESCE(storage_mb, -1), created_at, updated_at 
		 FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.IsSuspended, &u.MemoryMB, &u.CPUPercent, &u.MaxSites, &u.StorageMB, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) CreateUser(ctx context.Context, username string, isAdmin bool) (*User, error) {
	return db.CreateUserWithLimits(ctx, username, isAdmin, 512, 100, -1, -1)
}

func (db *DB) CreateUserWithLimits(ctx context.Context, username string, isAdmin bool, memoryMB, cpuPercent, maxSites, storageMB int) (*User, error) {
	result, err := db.ExecContext(ctx,
		"INSERT INTO users (username, is_admin, memory_mb, cpu_percent, max_sites, storage_mb) VALUES (?, ?, ?, ?, ?, ?)",
		username, isAdmin, memoryMB, cpuPercent, maxSites, storageMB,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:         id,
		Username:   username,
		IsAdmin:    isAdmin,
		MemoryMB:   memoryMB,
		CPUPercent: cpuPercent,
		MaxSites:   maxSites,
		StorageMB:  storageMB,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}

func (db *DB) EnsureUser(ctx context.Context, username string) (*User, error) {
	user, err := db.GetUser(ctx, username)
	if err == sql.ErrNoRows {
		// Root user is always admin
		// Also check for default admin user created during installation
		isAdmin := username == "root" || db.isDefaultAdmin(username)
		// fastcp admin should have full resources by default
		if username == "fastcp" {
			return db.CreateUserWithLimits(ctx, username, isAdmin, -1, -1, -1, -1)
		}
		return db.CreateUser(ctx, username, isAdmin)
	}
	return user, err
}

func (db *DB) isDefaultAdmin(username string) bool {
	data, err := os.ReadFile("/opt/fastcp/data/default_admin")
	if err != nil {
		return false
	}
	defaultAdmin := strings.TrimSpace(string(data))
	return defaultAdmin == username
}

func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, is_admin, COALESCE(is_suspended, 0), COALESCE(memory_mb, 512), COALESCE(cpu_percent, 100), 
		 COALESCE(max_sites, -1), COALESCE(storage_mb, -1), created_at, updated_at 
		 FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.IsSuspended, &u.MemoryMB, &u.CPUPercent, &u.MaxSites, &u.StorageMB, &u.CreatedAt, &u.UpdatedAt); err != nil {
			continue
		}
		users = append(users, &u)
	}
	return users, nil
}

func (db *DB) DeleteUser(ctx context.Context, username string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE username = ?", username)
	return err
}

func (db *DB) SetUserSuspended(ctx context.Context, username string, suspended bool) error {
	_, err := db.ExecContext(ctx,
		"UPDATE users SET is_suspended = ?, updated_at = CURRENT_TIMESTAMP WHERE username = ?",
		suspended, username,
	)
	return err
}

// GetSuspendedUsernames returns a list of all suspended usernames
func (db *DB) GetSuspendedUsernames(ctx context.Context) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT username FROM users WHERE is_suspended = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	suspended := make(map[string]bool)
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			continue
		}
		suspended[username] = true
	}
	return suspended, nil
}

func (db *DB) UpdateUserAdmin(ctx context.Context, username string, isAdmin bool) error {
	_, err := db.ExecContext(ctx,
		"UPDATE users SET is_admin = ?, updated_at = CURRENT_TIMESTAMP WHERE username = ?",
		isAdmin, username,
	)
	return err
}

func (db *DB) UpdateUserLimits(ctx context.Context, username string, memoryMB, cpuPercent, maxSites, storageMB int) error {
	_, err := db.ExecContext(ctx,
		`UPDATE users
		 SET memory_mb = ?, cpu_percent = ?, max_sites = ?, storage_mb = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE username = ?`,
		memoryMB, cpuPercent, maxSites, storageMB, username,
	)
	return err
}

// Session operations
func (db *DB) CreateSession(ctx context.Context, token, username string, expiresAt time.Time) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO sessions (token, username, expires_at) VALUES (?, ?, ?)",
		token, username, expiresAt,
	)
	return err
}

func (db *DB) GetSession(ctx context.Context, token string) (*Session, error) {
	var s Session
	err := db.QueryRowContext(ctx,
		"SELECT id, token, username, expires_at, created_at FROM sessions WHERE token = ? AND expires_at > ?",
		token, time.Now(),
	).Scan(&s.ID, &s.Token, &s.Username, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	return err
}

func (db *DB) CleanExpiredSessions(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", time.Now())
	return err
}

func (db *DB) CreateImpersonationSession(ctx context.Context, s *ImpersonationSession) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO impersonation_sessions
		 (token, admin_username, target_username, reason, ip_address, user_agent, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.Token, s.AdminUsername, s.TargetUsername, s.Reason, s.IPAddress, s.UserAgent, s.ExpiresAt,
	)
	return err
}

func (db *DB) GetImpersonationSession(ctx context.Context, token string) (*ImpersonationSession, error) {
	var s ImpersonationSession
	err := db.QueryRowContext(ctx,
		`SELECT token, admin_username, target_username, reason, ip_address, user_agent, expires_at, created_at
		 FROM impersonation_sessions
		 WHERE token = ? AND expires_at > ?`,
		token, time.Now(),
	).Scan(
		&s.Token, &s.AdminUsername, &s.TargetUsername, &s.Reason, &s.IPAddress, &s.UserAgent, &s.ExpiresAt, &s.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) DeleteImpersonationSession(ctx context.Context, token string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM impersonation_sessions WHERE token = ?", token)
	return err
}

func (db *DB) CleanExpiredImpersonationSessions(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "DELETE FROM impersonation_sessions WHERE expires_at < ?", time.Now())
	return err
}

func (db *DB) CreateImpersonationAuditLog(ctx context.Context, a *ImpersonationAuditLog) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO impersonation_audit_logs
		 (admin_username, target_username, action, token, reason, ip_address, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.AdminUsername, a.TargetUsername, a.Action, a.Token, a.Reason, a.IPAddress, a.UserAgent,
	)
	return err
}

// Site operations
func (db *DB) CreateSite(ctx context.Context, site *Site) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sites (id, username, domain, slug, site_type, document_root, ssl_enabled, force_https, compression_enabled, gzip_enabled, zstd_enabled, cache_control_enabled, cache_control_value, php_version, php_memory_limit, php_post_max_size, php_upload_max_filesize, php_max_execution_time, php_max_input_vars)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, site.Username, site.Domain, site.Slug, site.SiteType, site.DocumentRoot, site.SSLEnabled,
		site.ForceHTTPS, site.CompressionEnabled, site.GzipEnabled, site.ZstdEnabled, site.CacheControlEnabled, site.CacheControlValue, site.PHPVersion,
		site.PHPMemoryLimit, site.PHPPostMaxSize, site.PHPUploadMaxSize, site.PHPMaxExecutionTime, site.PHPMaxInputVars,
	)
	return err
}

func (db *DB) GetSite(ctx context.Context, id string) (*Site, error) {
	var s Site
	var slug sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, username, domain, COALESCE(slug, ''), site_type, document_root, ssl_enabled,
		        COALESCE(force_https, 1),
		        COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
		        COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''),
		        COALESCE(php_version, '8.4'),
		        COALESCE(php_memory_limit, '256M'),
		        COALESCE(php_post_max_size, '64M'),
		        COALESCE(php_upload_max_filesize, '64M'),
		        COALESCE(php_max_execution_time, 300),
		        COALESCE(php_max_input_vars, 5000),
		        created_at, updated_at
		 FROM sites WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.Username, &s.Domain, &slug, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled,
		&s.ForceHTTPS, &s.CompressionEnabled, &s.GzipEnabled, &s.ZstdEnabled, &s.CacheControlEnabled, &s.CacheControlValue, &s.PHPVersion,
		&s.PHPMemoryLimit, &s.PHPPostMaxSize, &s.PHPUploadMaxSize, &s.PHPMaxExecutionTime, &s.PHPMaxInputVars,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Slug = slug.String
	return &s, nil
}

func (db *DB) ListSites(ctx context.Context, username string) ([]*Site, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, domain, COALESCE(slug, ''), site_type, document_root, ssl_enabled,
		        COALESCE(force_https, 1),
		        COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
		        COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''),
		        COALESCE(php_version, '8.4'),
		        COALESCE(php_memory_limit, '256M'),
		        COALESCE(php_post_max_size, '64M'),
		        COALESCE(php_upload_max_filesize, '64M'),
		        COALESCE(php_max_execution_time, 300),
		        COALESCE(php_max_input_vars, 5000),
		        created_at, updated_at
		 FROM sites WHERE username = ? ORDER BY created_at DESC`,
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Username, &s.Domain, &s.Slug, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled,
			&s.ForceHTTPS, &s.CompressionEnabled, &s.GzipEnabled, &s.ZstdEnabled, &s.CacheControlEnabled, &s.CacheControlValue, &s.PHPVersion,
			&s.PHPMemoryLimit, &s.PHPPostMaxSize, &s.PHPUploadMaxSize, &s.PHPMaxExecutionTime, &s.PHPMaxInputVars,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, &s)
	}
	return sites, nil
}

func (db *DB) DeleteSite(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", id)
	return err
}

func (db *DB) CountUserSites(ctx context.Context, username string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites WHERE username = ?", username).Scan(&count)
	return count, err
}

// PaginatedResult holds paginated query results
type PaginatedResult struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// ListSitesPaginated returns paginated sites with optional search
func (db *DB) ListSitesPaginated(ctx context.Context, username string, page, limit int, search string) ([]*Site, int, error) {
	offset := (page - 1) * limit
	search = "%" + search + "%"

	// Get total count
	var total int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sites WHERE username = ? AND (domain LIKE ? OR slug LIKE ?)`,
		username, search, search,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, domain, COALESCE(slug, ''), site_type, document_root, ssl_enabled,
		        COALESCE(force_https, 1),
		        COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
		        COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''),
		        COALESCE(php_version, '8.4'),
		        COALESCE(php_memory_limit, '256M'),
		        COALESCE(php_post_max_size, '64M'),
		        COALESCE(php_upload_max_filesize, '64M'),
		        COALESCE(php_max_execution_time, 300),
		        COALESCE(php_max_input_vars, 5000),
		        created_at, updated_at
		 FROM sites WHERE username = ? AND (domain LIKE ? OR slug LIKE ?)
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		username, search, search, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Username, &s.Domain, &s.Slug, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled,
			&s.ForceHTTPS, &s.CompressionEnabled, &s.GzipEnabled, &s.ZstdEnabled, &s.CacheControlEnabled, &s.CacheControlValue, &s.PHPVersion,
			&s.PHPMemoryLimit, &s.PHPPostMaxSize, &s.PHPUploadMaxSize, &s.PHPMaxExecutionTime, &s.PHPMaxInputVars,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, err
		}
		sites = append(sites, &s)
	}
	return sites, total, nil
}

func (db *DB) ListAllSites(ctx context.Context) ([]*Site, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, domain, COALESCE(slug, ''), site_type, document_root, ssl_enabled,
		        COALESCE(force_https, 1),
		        COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
		        COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''),
		        COALESCE(php_version, '8.4'),
		        COALESCE(php_memory_limit, '256M'),
		        COALESCE(php_post_max_size, '64M'),
		        COALESCE(php_upload_max_filesize, '64M'),
		        COALESCE(php_max_execution_time, 300),
		        COALESCE(php_max_input_vars, 5000),
		        created_at, updated_at
		 FROM sites ORDER BY username, domain`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Username, &s.Domain, &s.Slug, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled,
			&s.ForceHTTPS, &s.CompressionEnabled, &s.GzipEnabled, &s.ZstdEnabled, &s.CacheControlEnabled, &s.CacheControlValue, &s.PHPVersion,
			&s.PHPMemoryLimit, &s.PHPPostMaxSize, &s.PHPUploadMaxSize, &s.PHPMaxExecutionTime, &s.PHPMaxInputVars,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, &s)
	}
	return sites, nil
}

func (db *DB) UpdateSiteSettings(ctx context.Context, siteID, username string, forceHTTPS, compressionEnabled, gzipEnabled, zstdEnabled, cacheControlEnabled bool, cacheControlValue, phpVersion, phpMemoryLimit, phpPostMaxSize, phpUploadMaxSize string, phpMaxExecutionTime, phpMaxInputVars int) error {
	result, err := db.ExecContext(ctx,
		`UPDATE sites
		 SET force_https = ?, compression_enabled = ?, gzip_enabled = ?, zstd_enabled = ?, cache_control_enabled = ?, cache_control_value = ?, php_version = ?, php_memory_limit = ?, php_post_max_size = ?, php_upload_max_filesize = ?, php_max_execution_time = ?, php_max_input_vars = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND username = ?`,
		forceHTTPS, compressionEnabled, gzipEnabled, zstdEnabled, cacheControlEnabled, cacheControlValue, phpVersion, phpMemoryLimit, phpPostMaxSize, phpUploadMaxSize, phpMaxExecutionTime, phpMaxInputVars, siteID, username,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SlugExists checks if a slug already exists for a username
func (db *DB) SlugExists(ctx context.Context, username, slug string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sites WHERE username = ? AND slug = ?",
		username, slug,
	).Scan(&count)
	return count > 0, err
}

// DomainExists checks if a domain is already in use (globally)
func (db *DB) DomainExists(ctx context.Context, domain string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM site_domains WHERE domain = ?",
		domain,
	).Scan(&count)
	return count > 0, err
}

// SiteDomain operations
func (db *DB) AddSiteDomain(ctx context.Context, domain *SiteDomain) error {
	result, err := db.ExecContext(ctx,
		`INSERT INTO site_domains (site_id, domain, is_primary, redirect_to_primary)
		 VALUES (?, ?, ?, ?)`,
		domain.SiteID, domain.Domain, domain.IsPrimary, domain.RedirectToPrimary,
	)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	domain.ID = id
	domain.CreatedAt = time.Now()
	return nil
}

func (db *DB) GetSiteDomains(ctx context.Context, siteID string) ([]*SiteDomain, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, site_id, domain, is_primary, COALESCE(redirect_to_primary, 0), created_at
		 FROM site_domains WHERE site_id = ? ORDER BY is_primary DESC, domain ASC`,
		siteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []*SiteDomain
	for rows.Next() {
		var d SiteDomain
		if err := rows.Scan(&d.ID, &d.SiteID, &d.Domain, &d.IsPrimary, &d.RedirectToPrimary, &d.CreatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, &d)
	}
	return domains, nil
}

func (db *DB) GetSiteDomain(ctx context.Context, id int64) (*SiteDomain, error) {
	var d SiteDomain
	err := db.QueryRowContext(ctx,
		`SELECT id, site_id, domain, is_primary, COALESCE(redirect_to_primary, 0), created_at
		 FROM site_domains WHERE id = ?`,
		id,
	).Scan(&d.ID, &d.SiteID, &d.Domain, &d.IsPrimary, &d.RedirectToPrimary, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (db *DB) UpdateSiteDomain(ctx context.Context, domain *SiteDomain) error {
	_, err := db.ExecContext(ctx,
		`UPDATE site_domains SET is_primary = ?, redirect_to_primary = ? WHERE id = ?`,
		domain.IsPrimary, domain.RedirectToPrimary, domain.ID,
	)
	return err
}

func (db *DB) SetPrimaryDomain(ctx context.Context, siteID string, domainID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear all primary flags for this site
	_, err = tx.ExecContext(ctx, "UPDATE site_domains SET is_primary = 0 WHERE site_id = ?", siteID)
	if err != nil {
		return err
	}

	// Set the new primary
	_, err = tx.ExecContext(ctx, "UPDATE site_domains SET is_primary = 1, redirect_to_primary = 0 WHERE id = ?", domainID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) DeleteSiteDomain(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM site_domains WHERE id = ?", id)
	return err
}

func (db *DB) GetPrimaryDomain(ctx context.Context, siteID string) (*SiteDomain, error) {
	var d SiteDomain
	err := db.QueryRowContext(ctx,
		`SELECT id, site_id, domain, is_primary, COALESCE(redirect_to_primary, 0), created_at
		 FROM site_domains WHERE site_id = ? AND is_primary = 1`,
		siteID,
	).Scan(&d.ID, &d.SiteID, &d.Domain, &d.IsPrimary, &d.RedirectToPrimary, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (db *DB) GetAllSiteDomainsGrouped(ctx context.Context) (map[string][]*SiteDomain, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, site_id, domain, is_primary, COALESCE(redirect_to_primary, 0), created_at
		 FROM site_domains ORDER BY site_id, is_primary DESC, domain ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]*SiteDomain)
	for rows.Next() {
		var d SiteDomain
		if err := rows.Scan(&d.ID, &d.SiteID, &d.Domain, &d.IsPrimary, &d.RedirectToPrimary, &d.CreatedAt); err != nil {
			return nil, err
		}
		result[d.SiteID] = append(result[d.SiteID], &d)
	}
	return result, nil
}

// Database operations
func (db *DB) CreateDatabase(ctx context.Context, database *Database) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO databases (id, username, db_name, db_user, db_password) VALUES (?, ?, ?, ?, ?)",
		database.ID, database.Username, database.DBName, database.DBUser, database.DBPassword,
	)
	return err
}

func (db *DB) GetDatabase(ctx context.Context, id string) (*Database, error) {
	var d Database
	err := db.QueryRowContext(ctx,
		"SELECT id, username, db_name, db_user, COALESCE(db_password, ''), created_at FROM databases WHERE id = ?",
		id,
	).Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.DBPassword, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (db *DB) ListDatabases(ctx context.Context, username string) ([]*Database, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, db_name, db_user, COALESCE(db_password, ''), created_at FROM databases WHERE username = ? ORDER BY created_at DESC",
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []*Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.DBPassword, &d.CreatedAt); err != nil {
			return nil, err
		}
		databases = append(databases, &d)
	}
	return databases, nil
}

// ListDatabasesPaginated returns paginated databases with optional search
func (db *DB) ListDatabasesPaginated(ctx context.Context, username string, page, limit int, search string) ([]*Database, int, error) {
	offset := (page - 1) * limit
	search = "%" + search + "%"

	// Get total count
	var total int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM databases WHERE username = ? AND (db_name LIKE ? OR db_user LIKE ?)`,
		username, search, search,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, db_name, db_user, COALESCE(db_password, ''), created_at 
		 FROM databases WHERE username = ? AND (db_name LIKE ? OR db_user LIKE ?)
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		username, search, search, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var databases []*Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.DBPassword, &d.CreatedAt); err != nil {
			return nil, 0, err
		}
		databases = append(databases, &d)
	}
	return databases, total, nil
}

func (db *DB) DeleteDatabase(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM databases WHERE id = ?", id)
	return err
}

func (db *DB) UpdateDatabasePassword(ctx context.Context, id, encryptedPassword string) error {
	_, err := db.ExecContext(ctx, "UPDATE databases SET db_password = ? WHERE id = ?", encryptedPassword, id)
	return err
}

// SSH Key operations
func (db *DB) CreateSSHKey(ctx context.Context, key *SSHKey) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO ssh_keys (id, username, name, public_key, fingerprint) VALUES (?, ?, ?, ?, ?)",
		key.ID, key.Username, key.Name, key.PublicKey, key.Fingerprint,
	)
	return err
}

func (db *DB) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	var k SSHKey
	err := db.QueryRowContext(ctx,
		"SELECT id, username, name, public_key, fingerprint, created_at FROM ssh_keys WHERE id = ?",
		id,
	).Scan(&k.ID, &k.Username, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (db *DB) ListSSHKeys(ctx context.Context, username string) ([]*SSHKey, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, name, public_key, fingerprint, created_at FROM ssh_keys WHERE username = ? ORDER BY created_at DESC",
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*SSHKey
	for rows.Next() {
		var k SSHKey
		if err := rows.Scan(&k.ID, &k.Username, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, &k)
	}
	return keys, nil
}

func (db *DB) DeleteSSHKey(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM ssh_keys WHERE id = ?", id)
	return err
}

// Cron Job operations
func (db *DB) CreateCronJob(ctx context.Context, job *CronJob) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO cron_jobs (id, username, name, expression, command, enabled) VALUES (?, ?, ?, ?, ?, ?)",
		job.ID, job.Username, job.Name, job.Expression, job.Command, job.Enabled,
	)
	return err
}

func (db *DB) GetCronJob(ctx context.Context, id string) (*CronJob, error) {
	var j CronJob
	err := db.QueryRowContext(ctx,
		"SELECT id, username, name, expression, command, enabled, created_at, updated_at FROM cron_jobs WHERE id = ?",
		id,
	).Scan(&j.ID, &j.Username, &j.Name, &j.Expression, &j.Command, &j.Enabled, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (db *DB) ListCronJobs(ctx context.Context, username string) ([]*CronJob, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, name, expression, command, enabled, created_at, updated_at FROM cron_jobs WHERE username = ? ORDER BY created_at DESC",
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*CronJob
	for rows.Next() {
		var j CronJob
		if err := rows.Scan(&j.ID, &j.Username, &j.Name, &j.Expression, &j.Command, &j.Enabled, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, nil
}

func (db *DB) UpdateCronJob(ctx context.Context, job *CronJob) error {
	_, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET name = ?, expression = ?, command = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		job.Name, job.Expression, job.Command, job.Enabled, job.ID,
	)
	return err
}

func (db *DB) ToggleCronJob(ctx context.Context, id string, enabled bool) error {
	_, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		enabled, id,
	)
	return err
}

func (db *DB) DeleteCronJob(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM cron_jobs WHERE id = ?", id)
	return err
}

func (db *DB) ListAllCronJobs(ctx context.Context) ([]*CronJob, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, name, expression, command, enabled, created_at, updated_at FROM cron_jobs ORDER BY username, created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*CronJob
	for rows.Next() {
		var j CronJob
		if err := rows.Scan(&j.ID, &j.Username, &j.Name, &j.Expression, &j.Command, &j.Enabled, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, nil
}
