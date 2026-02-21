package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/rehmatworks/fastcp/internal/database"
)

const (
	tokenLength   = 32
	sessionExpiry = 24 * time.Hour
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
		User: &User{
			ID:        dbUser.ID,
			Username:  dbUser.Username,
			IsAdmin:   dbUser.IsAdmin,
			CreatedAt: dbUser.CreatedAt,
		},
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

	return &User{
		ID:        dbUser.ID,
		Username:  dbUser.Username,
		IsAdmin:   dbUser.IsAdmin,
		CreatedAt: dbUser.CreatedAt,
	}, nil
}

func generateToken() (string, error) {
	bytes := make([]byte, tokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
