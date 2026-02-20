package database

import (
	"context"
	"database/sql"
	"fmt"
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
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

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

		`CREATE TABLE IF NOT EXISTS sites (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			domain TEXT UNIQUE NOT NULL,
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

		// Add resource limits to users table (safe to run multiple times - will fail silently if exists)
		`ALTER TABLE users ADD COLUMN memory_mb INTEGER DEFAULT 512`,
		`ALTER TABLE users ADD COLUMN cpu_percent INTEGER DEFAULT 100`,
		`ALTER TABLE users ADD COLUMN max_sites INTEGER DEFAULT -1`,
		`ALTER TABLE users ADD COLUMN storage_mb INTEGER DEFAULT -1`,
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
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	IsAdmin    bool      `json:"is_admin"`
	MemoryMB   int       `json:"memory_mb"`
	CPUPercent int       `json:"cpu_percent"`
	MaxSites   int       `json:"max_sites"`   // -1 = unlimited
	StorageMB  int       `json:"storage_mb"`  // -1 = unlimited
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Site represents a website
type Site struct {
	ID           string        `json:"id"`
	Username     string        `json:"username"`
	Domain       string        `json:"domain"`
	SiteType     string        `json:"site_type"`
	DocumentRoot string        `json:"document_root"`
	SSLEnabled   bool          `json:"ssl_enabled"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Domains      []*SiteDomain `json:"domains,omitempty"`
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
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	DBName    string    `json:"db_name"`
	DBUser    string    `json:"db_user"`
	CreatedAt time.Time `json:"created_at"`
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

// User operations
func (db *DB) GetUser(ctx context.Context, username string) (*User, error) {
	var u User
	err := db.QueryRowContext(ctx,
		`SELECT id, username, is_admin, COALESCE(memory_mb, 512), COALESCE(cpu_percent, 100), 
		 COALESCE(max_sites, -1), COALESCE(storage_mb, -1), created_at, updated_at 
		 FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.MemoryMB, &u.CPUPercent, &u.MaxSites, &u.StorageMB, &u.CreatedAt, &u.UpdatedAt)
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
		isAdmin := username == "root"
		return db.CreateUser(ctx, username, isAdmin)
	}
	return user, err
}

func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, is_admin, COALESCE(memory_mb, 512), COALESCE(cpu_percent, 100), 
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
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.MemoryMB, &u.CPUPercent, &u.MaxSites, &u.StorageMB, &u.CreatedAt, &u.UpdatedAt); err != nil {
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

func (db *DB) UpdateUserAdmin(ctx context.Context, username string, isAdmin bool) error {
	_, err := db.ExecContext(ctx,
		"UPDATE users SET is_admin = ?, updated_at = CURRENT_TIMESTAMP WHERE username = ?",
		isAdmin, username,
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

// Site operations
func (db *DB) CreateSite(ctx context.Context, site *Site) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sites (id, username, domain, site_type, document_root, ssl_enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		site.ID, site.Username, site.Domain, site.SiteType, site.DocumentRoot, site.SSLEnabled,
	)
	return err
}

func (db *DB) GetSite(ctx context.Context, id string) (*Site, error) {
	var s Site
	err := db.QueryRowContext(ctx,
		`SELECT id, username, domain, site_type, document_root, ssl_enabled, created_at, updated_at
		 FROM sites WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.Username, &s.Domain, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) ListSites(ctx context.Context, username string) ([]*Site, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, domain, site_type, document_root, ssl_enabled, created_at, updated_at
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
		if err := rows.Scan(&s.ID, &s.Username, &s.Domain, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

func (db *DB) ListAllSites(ctx context.Context) ([]*Site, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, domain, site_type, document_root, ssl_enabled, created_at, updated_at
		 FROM sites ORDER BY username, domain`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Username, &s.Domain, &s.SiteType, &s.DocumentRoot, &s.SSLEnabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, &s)
	}
	return sites, nil
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
		"INSERT INTO databases (id, username, db_name, db_user) VALUES (?, ?, ?, ?)",
		database.ID, database.Username, database.DBName, database.DBUser,
	)
	return err
}

func (db *DB) GetDatabase(ctx context.Context, id string) (*Database, error) {
	var d Database
	err := db.QueryRowContext(ctx,
		"SELECT id, username, db_name, db_user, created_at FROM databases WHERE id = ?",
		id,
	).Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (db *DB) ListDatabases(ctx context.Context, username string) ([]*Database, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, db_name, db_user, created_at FROM databases WHERE username = ? ORDER BY created_at DESC",
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []*Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.CreatedAt); err != nil {
			return nil, err
		}
		databases = append(databases, &d)
	}
	return databases, nil
}

func (db *DB) DeleteDatabase(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM databases WHERE id = ?", id)
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
