package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/database"
)

const (
	tokenLength                = 32
	sessionExpiry              = 24 * time.Hour
	impersonationSessionExpiry = 2 * time.Hour
)

// AuthService handles authentication
type AuthService struct {
	db *database.DB
}

// NewAuthService creates a new auth service
func NewAuthService(db *database.DB) *AuthService {
	return &AuthService{db: db}
}

// Login authenticates a user using PAM (or mock on macOS)
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	// Block root login for security
	if req.Username == "root" {
		return nil, fmt.Errorf("root login is disabled. Please use the 'fastcp' admin account or another non-root user")
	}

	// Authenticate using PAM (platform-specific)
	if err := s.pamAuth(req.Username, req.Password); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Ensure user exists in database
	dbUser, err := s.db.EnsureUser(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure user: %w", err)
	}

	// Generate session token
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Create session
	expiresAt := time.Now().Add(sessionExpiry)
	if err := s.db.CreateSession(ctx, token, req.Username, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &LoginResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      s.userFromDB(dbUser, nil),
	}, nil
}

// Logout invalidates a session
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.db.DeleteSession(ctx, token)
}

// ValidateToken validates a session token and returns the user
func (s *AuthService) ValidateToken(ctx context.Context, token string) (*User, error) {
	session, err := s.db.GetSession(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired token")
	}

	dbUser, err := s.db.GetUser(ctx, session.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	_ = s.db.CleanExpiredImpersonationSessions(ctx)
	var impersonation *database.ImpersonationSession
	impSession, impErr := s.db.GetImpersonationSession(ctx, token)
	if impErr == nil {
		impersonation = impSession
	} else if impErr != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to resolve impersonation context")
	}

	return s.userFromDB(dbUser, impersonation), nil
}

func (s *AuthService) userFromDB(dbUser *database.User, imp *database.ImpersonationSession) *User {
	u := &User{
		ID:          dbUser.ID,
		Username:    dbUser.Username,
		IsAdmin:     dbUser.IsAdmin,
		IsSuspended: dbUser.IsSuspended,
		MemoryMB:    dbUser.MemoryMB,
		CPUPercent:  dbUser.CPUPercent,
		MaxSites:    dbUser.MaxSites,
		StorageMB:   dbUser.StorageMB,
		CreatedAt:   dbUser.CreatedAt,
	}
	if imp != nil {
		u.IsImpersonating = true
		u.ImpersonatedBy = imp.AdminUsername
		u.ImpersonationReason = imp.Reason
		expires := imp.ExpiresAt.UTC()
		u.ImpersonationExpiresAt = &expires
	}
	return u
}

func (s *AuthService) VerifyCredentials(username, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("username and password are required")
	}
	return s.pamAuth(username, password)
}

func (s *AuthService) StartImpersonation(ctx context.Context, adminUsername string, req *ImpersonateRequest, ipAddress, userAgent string) (*ImpersonateResponse, error) {
	target := strings.TrimSpace(req.TargetUsername)
	if target == "" {
		return nil, fmt.Errorf("target username is required")
	}
	if target == "root" {
		return nil, fmt.Errorf("root impersonation is not allowed")
	}
	if target == strings.TrimSpace(adminUsername) {
		return nil, fmt.Errorf("you are already logged in as this user")
	}
	if strings.TrimSpace(req.AdminPassword) == "" {
		return nil, fmt.Errorf("admin password confirmation is required")
	}

	adminUser, err := s.db.GetUser(ctx, adminUsername)
	if err != nil {
		return nil, fmt.Errorf("admin user not found")
	}
	if !adminUser.IsAdmin {
		return nil, fmt.Errorf("admin access required")
	}
	if adminUser.IsSuspended {
		return nil, fmt.Errorf("suspended admins cannot start impersonation sessions")
	}
	targetUser, err := s.db.GetUser(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("target user not found")
	}
	if targetUser.IsAdmin {
		return nil, fmt.Errorf("impersonating admin users is not allowed")
	}
	if targetUser.IsSuspended {
		return nil, fmt.Errorf("cannot impersonate a suspended user")
	}
	if err := s.VerifyCredentials(adminUsername, req.AdminPassword); err != nil {
		_ = s.db.CreateImpersonationAuditLog(context.Background(), &database.ImpersonationAuditLog{
			AdminUsername:  adminUsername,
			TargetUsername: target,
			Action:         "start_failed",
			Reason:         "admin_password_verification_failed",
			IPAddress:      ipAddress,
			UserAgent:      userAgent,
		})
		return nil, fmt.Errorf("admin password verification failed")
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate impersonation token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(impersonationSessionExpiry)
	reason := strings.TrimSpace(req.Reason)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start impersonation transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (token, username, expires_at) VALUES (?, ?, ?)",
		token, target, expiresAt,
	); err != nil {
		return nil, fmt.Errorf("failed to create impersonation session: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO impersonation_sessions
		 (token, admin_username, target_username, reason, ip_address, user_agent, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, adminUsername, target, reason, strings.TrimSpace(ipAddress), strings.TrimSpace(userAgent), expiresAt,
	); err != nil {
		return nil, fmt.Errorf("failed to create impersonation metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO impersonation_audit_logs
		 (admin_username, target_username, action, token, reason, ip_address, user_agent)
		 VALUES (?, ?, 'start', ?, ?, ?, ?)`,
		adminUsername, target, token, reason, strings.TrimSpace(ipAddress), strings.TrimSpace(userAgent),
	); err != nil {
		return nil, fmt.Errorf("failed to write impersonation audit log: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit impersonation session: %w", err)
	}

	return &ImpersonateResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      s.userFromDB(targetUser, &database.ImpersonationSession{AdminUsername: adminUsername, TargetUsername: target, Reason: reason, ExpiresAt: expiresAt}),
		Message:   fmt.Sprintf("Now impersonating %s.", target),
	}, nil
}

func (s *AuthService) StopImpersonation(ctx context.Context, token, activeUsername, ipAddress, userAgent string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token is required")
	}
	impSession, err := s.db.GetImpersonationSession(ctx, token)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("active session is not an impersonation session")
		}
		return fmt.Errorf("failed to resolve impersonation session")
	}
	if impSession.TargetUsername != strings.TrimSpace(activeUsername) {
		return fmt.Errorf("impersonation session mismatch")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start impersonation stop transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM impersonation_sessions WHERE token = ?", token); err != nil {
		return fmt.Errorf("failed to remove impersonation metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token); err != nil {
		return fmt.Errorf("failed to remove impersonation session: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO impersonation_audit_logs
		 (admin_username, target_username, action, token, reason, ip_address, user_agent)
		 VALUES (?, ?, 'stop', ?, ?, ?, ?)`,
		impSession.AdminUsername, impSession.TargetUsername, token, impSession.Reason, strings.TrimSpace(ipAddress), strings.TrimSpace(userAgent),
	); err != nil {
		return fmt.Errorf("failed to write impersonation stop audit log: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to finalize impersonation stop: %w", err)
	}
	return nil
}

func generateToken() (string, error) {
	bytes := make([]byte, tokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
