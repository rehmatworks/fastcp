package database

import (
	"context"
	"time"
)

// Session is a signed-in browser. Only a hash of its token is stored.
type Session struct {
	TokenHash    string
	Username     string
	ActingAs     string // set while an admin impersonates another user
	ActingReason string
	ActingUntil  time.Time
	IPAddress    string
	UserAgent    string
	CreatedAt    time.Time
	LastSeenAt   time.Time
	ExpiresAt    time.Time
}

func (db *DB) CreateSession(ctx context.Context, s *Session) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO auth_sessions (token_hash, username, ip_address, user_agent, created_at, last_seen_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.TokenHash, s.Username, s.IPAddress, s.UserAgent, s.CreatedAt.Unix(), s.LastSeenAt.Unix(), s.ExpiresAt.Unix())
	return err
}

func (db *DB) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	var s Session
	var actingUntil, created, seen, expires int64
	err := db.QueryRowContext(ctx,
		`SELECT token_hash, username, acting_as, acting_reason, acting_until, ip_address, user_agent, created_at, last_seen_at, expires_at
		 FROM auth_sessions WHERE token_hash = ?`, tokenHash).
		Scan(&s.TokenHash, &s.Username, &s.ActingAs, &s.ActingReason, &actingUntil, &s.IPAddress, &s.UserAgent, &created, &seen, &expires)
	if err != nil {
		return nil, notFound(err)
	}
	s.ActingUntil = time.Unix(actingUntil, 0).UTC()
	s.CreatedAt = time.Unix(created, 0).UTC()
	s.LastSeenAt = time.Unix(seen, 0).UTC()
	s.ExpiresAt = time.Unix(expires, 0).UTC()
	return &s, nil
}

func (db *DB) TouchSession(ctx context.Context, tokenHash string, at time.Time) error {
	_, err := db.ExecContext(ctx, "UPDATE auth_sessions SET last_seen_at = ? WHERE token_hash = ?", at.Unix(), tokenHash)
	return err
}

func (db *DB) SetSessionActingAs(ctx context.Context, tokenHash, target, reason string, until time.Time) error {
	var u int64
	if target != "" {
		u = until.Unix()
	}
	res, err := db.ExecContext(ctx,
		"UPDATE auth_sessions SET acting_as = ?, acting_reason = ?, acting_until = ? WHERE token_hash = ?",
		target, reason, u, tokenHash)
	return affected(res, err)
}

func (db *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE token_hash = ?", tokenHash)
	return err
}

// RevokeUserSessions signs a user out everywhere and ends any impersonation
// of that user.
func (db *DB) RevokeUserSessions(ctx context.Context, username string) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE username = ?", username); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		"UPDATE auth_sessions SET acting_as = '', acting_reason = '', acting_until = 0 WHERE acting_as = ?", username)
	return err
}

// DeleteStaleSessions removes sessions past their absolute expiry or idle
// for longer than idle.
func (db *DB) DeleteStaleSessions(ctx context.Context, now time.Time, idle time.Duration) error {
	_, err := db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE expires_at <= ? OR last_seen_at <= ?",
		now.Unix(), now.Add(-idle).Unix())
	return err
}
