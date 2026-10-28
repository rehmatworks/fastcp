package database

import (
	"context"
	"database/sql"
	"time"
)

// User is a panel account. Every account is also a Linux user.
type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	IsAdmin     bool      `json:"is_admin"`
	IsSuspended bool      `json:"is_suspended"`
	MemoryMB    int       `json:"memory_mb"`   // -1 = unlimited
	CPUPercent  int       `json:"cpu_percent"` // -1 = unlimited
	MaxSites    int       `json:"max_sites"`   // -1 = unlimited
	StorageMB   int       `json:"storage_mb"`  // -1 = unlimited
	SiteCount   int       `json:"site_count"`
	CreatedAt   time.Time `json:"created_at"`
}

const userColumns = `u.id, u.username, COALESCE(u.is_admin, 0), COALESCE(u.is_suspended, 0),
	COALESCE(u.memory_mb, 512), COALESCE(u.cpu_percent, 100), COALESCE(u.max_sites, -1), COALESCE(u.storage_mb, -1),
	(SELECT COUNT(*) FROM sites s WHERE s.username = u.username), u.created_at`

// scanner is implemented by *sql.Row and *sql.Rows. Time columns must be
// selected directly (not through COALESCE) so the driver parses them.
type scanner interface{ Scan(...any) error }

func scanUser(sc scanner) (*User, error) {
	var u User
	var created sql.NullTime
	err := sc.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.IsSuspended, &u.MemoryMB, &u.CPUPercent, &u.MaxSites, &u.StorageMB, &u.SiteCount, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = created.Time.UTC()
	return &u, nil
}

func (db *DB) GetUser(ctx context.Context, username string) (*User, error) {
	u, err := scanUser(db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users u WHERE u.username = ?", username))
	return u, notFound(err)
}

func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+userColumns+" FROM users u ORDER BY u.username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) CreateUser(ctx context.Context, u *User) error {
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx,
		`INSERT INTO users (username, is_admin, is_suspended, memory_mb, cpu_percent, max_sites, storage_mb, created_at, updated_at)
		 VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.IsAdmin, u.MemoryMB, u.CPUPercent, u.MaxSites, u.StorageMB, now, now)
	if err != nil {
		return conflict(err)
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt = now
	return nil
}

func (db *DB) UpdateUser(ctx context.Context, u *User) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET is_admin = ?, memory_mb = ?, cpu_percent = ?, max_sites = ?, storage_mb = ?, updated_at = ?
		 WHERE username = ?`,
		u.IsAdmin, u.MemoryMB, u.CPUPercent, u.MaxSites, u.StorageMB, time.Now().UTC(), u.Username)
	return affected(res, err)
}

func (db *DB) SetUserSuspended(ctx context.Context, username string, suspended bool) error {
	res, err := db.ExecContext(ctx, "UPDATE users SET is_suspended = ?, updated_at = ? WHERE username = ?",
		suspended, time.Now().UTC(), username)
	return affected(res, err)
}

func (db *DB) DeleteUser(ctx context.Context, username string) error {
	res, err := db.ExecContext(ctx, "DELETE FROM users WHERE username = ?", username)
	return affected(res, err)
}

// CountActiveAdmins counts administrators that are not suspended.
func (db *DB) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE is_admin = 1 AND COALESCE(is_suspended, 0) = 0").Scan(&n)
	return n, err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
