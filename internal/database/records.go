package database

import (
	"context"
	"database/sql"
	"time"
)

// Database is a MySQL database owned by a user.
type Database struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DBName      string    `json:"db_name"`
	DBUser      string    `json:"db_user"`
	PasswordEnc string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
}

const databaseColumns = "id, username, db_name, db_user, COALESCE(db_password, ''), created_at"

func scanDatabase(sc scanner) (*Database, error) {
	var d Database
	var created sql.NullTime
	if err := sc.Scan(&d.ID, &d.Username, &d.DBName, &d.DBUser, &d.PasswordEnc, &created); err != nil {
		return nil, err
	}
	d.CreatedAt = created.Time.UTC()
	return &d, nil
}

func (db *DB) CreateDatabase(ctx context.Context, d *Database) error {
	d.CreatedAt = time.Now().UTC()
	_, err := db.ExecContext(ctx,
		"INSERT INTO databases (id, username, db_name, db_user, db_password, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		d.ID, d.Username, d.DBName, d.DBUser, d.PasswordEnc, d.CreatedAt)
	return conflict(err)
}

func (db *DB) GetDatabase(ctx context.Context, id string) (*Database, error) {
	d, err := scanDatabase(db.QueryRowContext(ctx, "SELECT "+databaseColumns+" FROM databases WHERE id = ?", id))
	return d, notFound(err)
}

func (db *DB) ListDatabases(ctx context.Context, username string) ([]*Database, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+databaseColumns+" FROM databases WHERE username = ? ORDER BY db_name", username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Database
	for rows.Next() {
		d, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) CountDatabases(ctx context.Context, username string) (int, error) {
	var n int
	q := "SELECT COUNT(*) FROM databases"
	args := []any{}
	if username != "" {
		q += " WHERE username = ?"
		args = append(args, username)
	}
	err := db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// DatabaseNameTaken reports whether a database name or MySQL account name
// is already recorded.
func (db *DB) DatabaseNameTaken(ctx context.Context, name, user string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM databases WHERE db_name = ? OR db_user = ?", name, user).Scan(&n)
	return n > 0, err
}

func (db *DB) SetDatabasePassword(ctx context.Context, id, enc string) error {
	res, err := db.ExecContext(ctx, "UPDATE databases SET db_password = ? WHERE id = ?", enc, id)
	return affected(res, err)
}

func (db *DB) DeleteDatabase(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, "DELETE FROM databases WHERE id = ?", id)
	return affected(res, err)
}

// SSHKey is a public key installed in a user's authorized_keys.
type SSHKey struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

const sshKeyColumns = "id, username, name, public_key, fingerprint, created_at"

func scanSSHKey(sc scanner) (*SSHKey, error) {
	var k SSHKey
	var created sql.NullTime
	if err := sc.Scan(&k.ID, &k.Username, &k.Name, &k.PublicKey, &k.Fingerprint, &created); err != nil {
		return nil, err
	}
	k.CreatedAt = created.Time.UTC()
	return &k, nil
}

func (db *DB) CreateSSHKey(ctx context.Context, k *SSHKey) error {
	k.CreatedAt = time.Now().UTC()
	_, err := db.ExecContext(ctx,
		"INSERT INTO ssh_keys (id, username, name, public_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		k.ID, k.Username, k.Name, k.PublicKey, k.Fingerprint, k.CreatedAt)
	return conflict(err)
}

func (db *DB) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	k, err := scanSSHKey(db.QueryRowContext(ctx, "SELECT "+sshKeyColumns+" FROM ssh_keys WHERE id = ?", id))
	return k, notFound(err)
}

func (db *DB) ListSSHKeys(ctx context.Context, username string) ([]*SSHKey, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+sshKeyColumns+" FROM ssh_keys WHERE username = ? ORDER BY created_at DESC", username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SSHKey
	for rows.Next() {
		k, err := scanSSHKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (db *DB) DeleteSSHKey(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, "DELETE FROM ssh_keys WHERE id = ?", id)
	return affected(res, err)
}

// CronJob is a scheduled command in a user's crontab.
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

const cronColumns = "id, username, name, expression, command, COALESCE(enabled, 1), created_at, updated_at"

func scanCron(sc scanner) (*CronJob, error) {
	var j CronJob
	var created, updated sql.NullTime
	if err := sc.Scan(&j.ID, &j.Username, &j.Name, &j.Expression, &j.Command, &j.Enabled, &created, &updated); err != nil {
		return nil, err
	}
	j.CreatedAt, j.UpdatedAt = created.Time.UTC(), updated.Time.UTC()
	return &j, nil
}

func (db *DB) queryCron(ctx context.Context, where string, args ...any) ([]*CronJob, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+cronColumns+" FROM cron_jobs "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CronJob
	for rows.Next() {
		j, err := scanCron(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (db *DB) ListCronJobs(ctx context.Context, username string) ([]*CronJob, error) {
	return db.queryCron(ctx, "WHERE username = ? ORDER BY created_at", username)
}

func (db *DB) ListAllCronJobs(ctx context.Context) ([]*CronJob, error) {
	return db.queryCron(ctx, "ORDER BY username, created_at")
}

func (db *DB) GetCronJob(ctx context.Context, id string) (*CronJob, error) {
	j, err := scanCron(db.QueryRowContext(ctx, "SELECT "+cronColumns+" FROM cron_jobs WHERE id = ?", id))
	return j, notFound(err)
}

func (db *DB) CreateCronJob(ctx context.Context, j *CronJob) error {
	now := time.Now().UTC()
	j.CreatedAt, j.UpdatedAt = now, now
	_, err := db.ExecContext(ctx,
		"INSERT INTO cron_jobs (id, username, name, expression, command, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		j.ID, j.Username, j.Name, j.Expression, j.Command, j.Enabled, now, now)
	return conflict(err)
}

func (db *DB) UpdateCronJob(ctx context.Context, j *CronJob) error {
	j.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET name = ?, expression = ?, command = ?, enabled = ?, updated_at = ? WHERE id = ?",
		j.Name, j.Expression, j.Command, j.Enabled, j.UpdatedAt, j.ID)
	return affected(res, err)
}

func (db *DB) DeleteCronJob(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, "DELETE FROM cron_jobs WHERE id = ?", id)
	return affected(res, err)
}

// AuditEntry records an impersonation event.
type AuditEntry struct {
	Admin     string
	Target    string
	Action    string
	Reason    string
	IPAddress string
	UserAgent string
}

func (db *DB) AddAudit(ctx context.Context, a *AuditEntry) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO impersonation_audit_logs (admin_username, target_username, action, reason, ip_address, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Admin, a.Target, a.Action, a.Reason, a.IPAddress, a.UserAgent, time.Now().UTC())
	return err
}
