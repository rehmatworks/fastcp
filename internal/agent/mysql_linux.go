//go:build linux

package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	mysqlSocket  = "/var/run/mysqld/mysqld.sock"
	mysqlCnfPath = "/etc/mysql/conf.d/fastcp.cnf"
)

var dbPasswordRE = regexp.MustCompile(`^[A-Za-z0-9!#%+,.:=?@^_~-]{12,128}$`)

var dbHosts = []string{"localhost", "127.0.0.1"}

func openMySQL(ctx context.Context) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Net = "unix"
	cfg.Addr = mysqlSocket
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 2 * time.Minute
	cfg.WriteTimeout = 2 * time.Minute
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not connect to MySQL: %w", err)
	}
	return db, nil
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quoteString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func account(user, host string) string { return quoteString(user) + "@" + quoteString(host) }

func validateDBRequest(dbName, dbUser, password string) error {
	if err := validate.DBName(dbName); err != nil {
		return invalid(err)
	}
	if err := validate.DBUser(dbUser); err != nil {
		return invalid(err)
	}
	if validate.IsReservedDBName(dbName) {
		return deniedf("database %s is reserved", dbName)
	}
	if validate.IsReservedDBUser(dbUser) {
		return deniedf("database user %s is reserved", dbUser)
	}
	if password != "" && !dbPasswordRE.MatchString(password) {
		return invalidf("database password must be 12-128 characters of letters, digits and !#%%+,.:=?@^_~-")
	}
	return nil
}

func dbExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&n)
	return n > 0, err
}

func dbUserExists(ctx context.Context, db *sql.DB, user string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User = ?", user).Scan(&n)
	return n > 0, err
}

// createDatabase creates a database and a user with access to it only. It
// refuses to touch existing databases or users and undoes partial work.
func createDatabase(ctx context.Context, dbName, dbUser, password string) (err error) {
	if password == "" {
		return invalidf("a database password is required")
	}
	if err := validateDBRequest(dbName, dbUser, password); err != nil {
		return err
	}
	db, err := openMySQL(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if ok, err := dbExists(ctx, db, dbName); err != nil {
		return err
	} else if ok {
		return conflictf("database %s already exists", dbName)
	}
	if ok, err := dbUserExists(ctx, db, dbUser); err != nil {
		return err
	} else if ok {
		return conflictf("database user %s already exists", dbUser)
	}

	var undo []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(undo) - 1; i >= 0; i-- {
			if _, uerr := db.ExecContext(context.WithoutCancel(ctx), undo[i]); uerr != nil {
				slog.Warn("database rollback step failed", "error", uerr)
			}
		}
	}()
	if _, err = db.ExecContext(ctx, "CREATE DATABASE "+quoteIdent(dbName)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("creating database failed: %w", err)
	}
	undo = append(undo, "DROP DATABASE IF EXISTS "+quoteIdent(dbName))
	for _, host := range dbHosts {
		if _, err = db.ExecContext(ctx, "CREATE USER "+account(dbUser, host)+" IDENTIFIED BY "+quoteString(password)); err != nil {
			return fmt.Errorf("creating database user failed: %w", err)
		}
		undo = append(undo, "DROP USER IF EXISTS "+account(dbUser, host))
		if _, err = db.ExecContext(ctx, "GRANT ALL PRIVILEGES ON "+quoteIdent(dbName)+".* TO "+account(dbUser, host)); err != nil {
			return fmt.Errorf("granting privileges failed: %w", err)
		}
	}
	return nil
}

func dropDatabase(ctx context.Context, dbName, dbUser string) error {
	if err := validateDBRequest(dbName, dbUser, ""); err != nil {
		return err
	}
	db, err := openMySQL(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	var errs []error
	for _, host := range dbHosts {
		if _, err := db.ExecContext(ctx, "DROP USER IF EXISTS "+account(dbUser, host)); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(dbName)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *Agent) handleCreateDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[CreateDatabaseRequest](params)
	if err != nil {
		return nil, err
	}
	if err := createDatabase(ctx, req.DBName, req.DBUser, req.Password); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (a *Agent) handleDeleteDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[DatabaseTarget](params)
	if err != nil {
		return nil, err
	}
	if err := dropDatabase(ctx, req.DBName, req.DBUser); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (a *Agent) handleResetDatabasePassword(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[ResetDatabasePasswordRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.DBUser(req.DBUser); err != nil {
		return nil, invalid(err)
	}
	if validate.IsReservedDBUser(req.DBUser) {
		return nil, deniedf("database user %s is reserved", req.DBUser)
	}
	if !dbPasswordRE.MatchString(req.Password) {
		return nil, invalidf("database password must be 12-128 characters of letters, digits and !#%%+,.:=?@^_~-")
	}
	db, err := openMySQL(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if ok, err := dbUserExists(ctx, db, req.DBUser); err != nil {
		return nil, err
	} else if !ok {
		return nil, notFoundf("database user %s does not exist", req.DBUser)
	}
	for _, host := range dbHosts {
		if _, err := db.ExecContext(ctx, "ALTER USER IF EXISTS "+account(req.DBUser, host)+" IDENTIFIED BY "+quoteString(req.Password)); err != nil {
			return nil, fmt.Errorf("changing the password failed: %w", err)
		}
	}
	return map[string]string{"status": "ok"}, nil
}

// ---------------------------------------------------------------------------
// Server tuning

func defaultMySQLConfig(ram int) MySQLConfig {
	cfg := MySQLConfig{BufferPoolMB: 128, MaxConnections: 30, DetectedRAMMB: ram}
	switch {
	case ram >= 8192:
		cfg.BufferPoolMB, cfg.MaxConnections = 1024, 150
	case ram >= 4096:
		cfg.BufferPoolMB, cfg.MaxConnections = 512, 100
	case ram >= 2048:
		cfg.BufferPoolMB, cfg.MaxConnections = 256, 60
	}
	return cfg
}

func renderMySQLConfig(cfg MySQLConfig) string {
	perf := "OFF"
	if cfg.PerfSchema {
		perf = "ON"
	}
	return fmt.Sprintf(`# Generated by FastCP. Change these values in the FastCP settings.
[mysqld]
innodb_buffer_pool_size = %dM
innodb_log_buffer_size = 8M
innodb_flush_log_at_trx_commit = 2
innodb_flush_method = O_DIRECT
key_buffer_size = 4M
max_connections = %d
table_open_cache = %d
table_definition_cache = %d
thread_cache_size = 8
performance_schema = %s
skip-name-resolve
`, cfg.BufferPoolMB, cfg.MaxConnections, mysqlTableOpenCache, mysqlTableDefinitionCache, perf)
}

// The table caches hold the tables of many WordPress sites, a dozen or more
// each. Too small, and MySQL reopens tables on most queries.
const (
	mysqlTableOpenCache       = 4000
	mysqlTableDefinitionCache = 2000
)

func readMySQLConfig(ram int) MySQLConfig {
	cfg := defaultMySQLConfig(ram)
	data, err := os.ReadFile(mysqlCnfPath)
	if err != nil {
		return cfg
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "innodb_buffer_pool_size":
			var n int
			if _, err := fmt.Sscanf(v, "%dM", &n); err == nil && n > 0 {
				cfg.BufferPoolMB = n
			}
		case "max_connections":
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
				cfg.MaxConnections = n
			}
		case "performance_schema":
			cfg.PerfSchema = strings.EqualFold(v, "ON")
		}
	}
	return cfg
}

// ensureMySQLTuning writes the default tuning once on fresh installs. A
// FastCP file from an older version is rewritten with its values kept, and
// the running server resizes its table caches without a restart.
func (a *Agent) ensureMySQLTuning(ctx context.Context) {
	if old, err := os.ReadFile(mysqlCnfPath); err == nil {
		want := renderMySQLConfig(readMySQLConfig(systemRAMMB()))
		if string(old) == want || !strings.HasPrefix(string(old), "# Generated by FastCP.") {
			return
		}
		if err := writeFileAtomic(mysqlCnfPath, []byte(want), 0o644); err != nil {
			slog.Warn("could not update MySQL tuning", "error", err)
			return
		}
		if err := setMySQLTableCaches(ctx); err != nil {
			slog.Warn("MySQL table caches will change at its next restart", "error", err)
		}
		return
	}
	if _, err := os.Stat("/etc/mysql/conf.d"); err != nil {
		return
	}
	if err := writeFileAtomic(mysqlCnfPath, []byte(renderMySQLConfig(defaultMySQLConfig(systemRAMMB()))), 0o644); err != nil {
		slog.Warn("could not write MySQL tuning", "error", err)
		return
	}
	if err := a.restartMySQL(ctx); err != nil {
		slog.Warn("could not restart MySQL after tuning", "error", err)
	}
}

func setMySQLTableCaches(ctx context.Context) error {
	db, err := openMySQL(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, fmt.Sprintf("SET GLOBAL table_open_cache = %d, table_definition_cache = %d",
		mysqlTableOpenCache, mysqlTableDefinitionCache))
	return err
}

func (a *Agent) handleGetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	cfg := readMySQLConfig(systemRAMMB())
	return &cfg, nil
}

func (a *Agent) handleSetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[MySQLConfig](params)
	if err != nil {
		return nil, err
	}
	ram := systemRAMMB()
	if req.BufferPoolMB < 16 || req.BufferPoolMB > 65536 {
		return nil, invalidf("buffer pool must be between 16 and 65536 MB")
	}
	if ram > 0 && req.BufferPoolMB > ram*3/4 {
		return nil, invalidf("buffer pool cannot exceed 75%% of system memory (%d MB)", ram*3/4)
	}
	if req.MaxConnections < 5 || req.MaxConnections > 5000 {
		return nil, invalidf("max connections must be between 5 and 5000")
	}
	content := renderMySQLConfig(*req)
	old, readErr := os.ReadFile(mysqlCnfPath)
	if readErr == nil && string(old) == content {
		cfg := readMySQLConfig(ram)
		return &cfg, nil
	}
	if err := writeFileAtomic(mysqlCnfPath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	restore := func() {
		if readErr == nil {
			_ = writeFileAtomic(mysqlCnfPath, old, 0o644)
		} else {
			_ = os.Remove(mysqlCnfPath)
		}
	}
	if out, err := run(ctx, time.Minute, "mysqld", "--validate-config"); err != nil && !strings.Contains(out, "unknown option '--validate-config'") {
		restore()
		return nil, invalidf("MySQL rejected the configuration%s", tail(out, 1000))
	}
	if err := a.restartMySQL(ctx); err != nil {
		restore()
		_ = a.restartMySQL(ctx)
		return nil, fmt.Errorf("MySQL did not restart with the new settings and the previous settings were restored: %w", err)
	}
	cfg := readMySQLConfig(ram)
	return &cfg, nil
}

func (a *Agent) restartMySQL(ctx context.Context) error {
	if a.systemd {
		var last error
		for _, svc := range []string{"mysql", "mysqld", "mariadb"} {
			if !a.unitExists(svc) {
				continue
			}
			if err := a.systemctl(ctx, "restart", svc); err != nil {
				last = err
				continue
			}
			return nil
		}
		if last != nil {
			return last
		}
	}
	for _, svc := range []string{"mysql", "mariadb"} {
		if _, err := run(ctx, 2*time.Minute, "service", svc, "restart"); err == nil {
			return nil
		}
	}
	return errors.New("no MySQL service found")
}

func mysqlVersion(ctx context.Context) string {
	out, err := run(ctx, 10*time.Second, "mysql", "--version")
	if err != nil {
		return ""
	}
	return parseMySQLVersion(out)
}
