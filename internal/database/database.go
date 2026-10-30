// Package database stores the panel's state in SQLite.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SchemaVersion is the PRAGMA user_version this build writes.
const SchemaVersion = 3

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	ErrLimit    = errors.New("limit reached")
)

// DB wraps the SQLite database.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the database at path and migrates it.
func Open(path string) (*DB, error) {
	dsn := "file:" + path + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on&_busy_timeout=10000&_txlock=immediate"
	sqldb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	sqldb.SetConnMaxIdleTime(10 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := migrate(ctx, sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
	return &DB{sqldb}, nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// isUniqueViolation matches SQLite's message for UNIQUE and PRIMARY KEY
// violations, which is stable across driver versions.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func conflict(err error) error {
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// InTx runs fn in a write transaction.
func (db *DB) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Migrations

// v2Tables is the final FastCP 2 schema. Every statement is idempotent so it
// also upgrades databases written by any FastCP 2 release.
var v2Tables = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		is_admin INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		slug TEXT,
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
	`CREATE TABLE IF NOT EXISTS backup_configs (
		username TEXT PRIMARY KEY,
		repository TEXT NOT NULL DEFAULT '',
		password_enc TEXT NOT NULL DEFAULT '',
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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
	)`,
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
}

type column struct{ table, name, def string }

// v2Columns were added by ALTER TABLE during FastCP 2's lifetime.
var v2Columns = []column{
	{"site_domains", "redirect_to_primary", "INTEGER DEFAULT 0"},
	{"users", "memory_mb", "INTEGER DEFAULT 512"},
	{"users", "cpu_percent", "INTEGER DEFAULT 100"},
	{"users", "max_sites", "INTEGER DEFAULT -1"},
	{"users", "storage_mb", "INTEGER DEFAULT -1"},
	{"users", "is_suspended", "INTEGER DEFAULT 0"},
	{"sites", "slug", "TEXT"},
	{"databases", "db_password", "TEXT DEFAULT ''"},
	{"sites", "compression_enabled", "INTEGER DEFAULT 1"},
	{"sites", "gzip_enabled", "INTEGER DEFAULT 1"},
	{"sites", "zstd_enabled", "INTEGER DEFAULT 1"},
	{"sites", "cache_control_enabled", "INTEGER DEFAULT 0"},
	{"sites", "cache_control_value", "TEXT DEFAULT ''"},
	{"sites", "php_version", "TEXT DEFAULT '8.4'"},
	{"sites", "force_https", "INTEGER DEFAULT 1"},
	{"sites", "php_memory_limit", "TEXT DEFAULT '256M'"},
	{"sites", "php_post_max_size", "TEXT DEFAULT '64M'"},
	{"sites", "php_upload_max_filesize", "TEXT DEFAULT '64M'"},
	{"sites", "php_max_execution_time", "INTEGER DEFAULT 300"},
	{"sites", "php_max_input_vars", "INTEGER DEFAULT 5000"},
	{"backup_configs", "last_prune_at", "DATETIME"},
	{"backup_configs", "backend_type", "TEXT NOT NULL DEFAULT 'sftp'"},
	{"backup_configs", "sftp_username", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "sftp_host", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "sftp_port", "INTEGER NOT NULL DEFAULT 22"},
	{"backup_configs", "sftp_path", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_endpoint", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_bucket", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_prefix", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_region", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_bucket_lookup", "TEXT NOT NULL DEFAULT 'auto'"},
	{"backup_configs", "s3_list_objects_v1", "INTEGER NOT NULL DEFAULT 0"},
	{"backup_configs", "s3_access_key_enc", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_secret_key_enc", "TEXT NOT NULL DEFAULT ''"},
	{"backup_configs", "s3_session_token_enc", "TEXT NOT NULL DEFAULT ''"},
}

var v2Indexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_sites_username ON sites(username)`,
	`CREATE INDEX IF NOT EXISTS idx_databases_username ON databases(username)`,
	`CREATE INDEX IF NOT EXISTS idx_ssh_keys_username ON ssh_keys(username)`,
	`CREATE INDEX IF NOT EXISTS idx_cron_jobs_username ON cron_jobs(username)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_username_slug ON sites(username, slug)`,
	`CREATE INDEX IF NOT EXISTS idx_backup_configs_enabled_next_run ON backup_configs(enabled, next_run_at)`,
	`CREATE INDEX IF NOT EXISTS idx_backup_jobs_username_started ON backup_jobs(username, started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_impersonation_audit_admin_created ON impersonation_audit_logs(admin_username, created_at DESC)`,
}

// v3Statements replace FastCP 2's plaintext session tokens. Everyone signs
// in again after the upgrade.
var v3Statements = []string{
	`DROP TABLE IF EXISTS impersonation_sessions`,
	`DROP TABLE IF EXISTS sessions`,
	`CREATE TABLE IF NOT EXISTS auth_sessions (
		token_hash TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		acting_as TEXT NOT NULL DEFAULT '',
		acting_reason TEXT NOT NULL DEFAULT '',
		acting_until INTEGER NOT NULL DEFAULT 0,
		ip_address TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_auth_sessions_username ON auth_sessions(username)`,
	`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at)`,
}

var v3Columns = []column{
	// force_https_auto: turn on HTTPS redirects once a certificate works,
	// until the owner sets the option explicitly.
	{"sites", "force_https_auto", "INTEGER NOT NULL DEFAULT 1"},
	{"backup_jobs", "artifact", "TEXT NOT NULL DEFAULT ''"},
}

// laterColumns were added after schema version 3. Builds without them
// ignore them, so the version is unchanged and the previous version still
// opens the database when an update is rolled back.
var laterColumns = []column{
	{"sites", "proxy_url", "TEXT NOT NULL DEFAULT ''"},
	{"sites", "page_cache", "INTEGER NOT NULL DEFAULT 0"},
	// Unused since 3.2.7, but 3.2.5 and 3.2.6 read it.
	{"sites", "page_cache_phones", "INTEGER NOT NULL DEFAULT 0"},
}

func hasColumn(ctx context.Context, tx *sql.Tx, table, name string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false, err
		}
		if strings.EqualFold(n, name) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func addColumns(ctx context.Context, tx *sql.Tx, cols []column) error {
	for _, c := range cols {
		ok, err := hasColumn(ctx, tx, c.table, c.name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.name, c.def)); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.name, err)
		}
	}
	return nil
}

func execAll(ctx context.Context, tx *sql.Tx, stmts []string) error {
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			head := strings.Join(strings.Fields(s), " ")
			if len(head) > 60 {
				head = head[:60]
			}
			return fmt.Errorf("%s: %w", head, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("the database schema (version %d) is newer than this FastCP build supports (%d)", version, SchemaVersion)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == SchemaVersion {
		if err := addColumns(ctx, tx, laterColumns); err != nil {
			return err
		}
		return tx.Commit()
	}

	if version < 2 {
		if err := execAll(ctx, tx, v2Tables); err != nil {
			return err
		}
		if err := addColumns(ctx, tx, v2Columns); err != nil {
			return err
		}
		if err := execAll(ctx, tx, v2Indexes); err != nil {
			return err
		}
	}
	if version < 3 {
		if err := execAll(ctx, tx, v3Statements); err != nil {
			return err
		}
		if err := addColumns(ctx, tx, v3Columns); err != nil {
			return err
		}
		if err := fillMissingSlugs(ctx, tx); err != nil {
			return err
		}
		if err := syncPrimaryDomains(ctx, tx); err != nil {
			return err
		}
	}
	if err := addColumns(ctx, tx, laterColumns); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// fillMissingSlugs derives slugs for sites created before FastCP stored
// them, from document roots of the form /home/<user>/apps/<slug>/public.
func fillMissingSlugs(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, username, document_root FROM sites WHERE slug IS NULL OR slug = ''")
	if err != nil {
		return err
	}
	type fix struct{ id, slug string }
	var fixes []fix
	for rows.Next() {
		var id, user, root string
		if err := rows.Scan(&id, &user, &root); err != nil {
			rows.Close()
			return err
		}
		prefix := "/home/" + user + "/apps/"
		if rest, ok := strings.CutPrefix(root, prefix); ok {
			if slug, ok := strings.CutSuffix(rest, "/public"); ok && slug != "" && !strings.Contains(slug, "/") {
				fixes = append(fixes, fix{id, slug})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range fixes {
		if _, err := tx.ExecContext(ctx, "UPDATE sites SET slug = ? WHERE id = ?", f.slug, f.id); err != nil {
			return err
		}
	}
	return nil
}

// syncPrimaryDomains copies each site's primary domain into sites.domain,
// which FastCP 2 did not update when the primary domain changed.
func syncPrimaryDomains(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id, d.domain FROM sites s
		JOIN site_domains d ON d.site_id = s.id AND d.is_primary = 1
		WHERE d.domain <> s.domain`)
	if err != nil {
		return err
	}
	var fixes [][2]string
	for rows.Next() {
		var id, domain string
		if err := rows.Scan(&id, &domain); err != nil {
			rows.Close()
			return err
		}
		fixes = append(fixes, [2]string{id, domain})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range fixes {
		if _, err := tx.ExecContext(ctx, "UPDATE sites SET domain = ? WHERE id = ?", f[1], f[0]); err != nil && !isUniqueViolation(err) {
			return err
		}
	}
	return nil
}
