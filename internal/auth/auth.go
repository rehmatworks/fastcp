package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrAPIKeyNotFound     = errors.New("api key not found")
	ErrAPIKeyExpired      = errors.New("api key expired")
	ErrUserNotAllowed     = errors.New("user not allowed to access FastCP")
)

// AllowedGroups defines which Unix groups can access FastCP
// Users in these groups can log in to the control panel
var AllowedGroups = []string{"root", "sudo", "wheel", "admin", "fastcp"}

// AdminGroups defines which groups get admin role
var AdminGroups = []string{"root", "sudo", "wheel"}

// Claims represents JWT claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Authenticate authenticates a user with username and password
// Uses Unix/PAM authentication on Linux, falls back to config-based auth
func Authenticate(username, password string) (*models.User, error) {
	// First try Unix authentication (Linux only)
	if runtime.GOOS == "linux" {
		if user, err := authenticateUnix(username, password); err == nil {
			return user, nil
		}
	}

	// Fallback to config-based authentication (for dev mode or non-Linux)
	return authenticateConfig(username, password)
}

// authenticateUnix authenticates against Unix/PAM
func authenticateUnix(username, password string) (*models.User, error) {
	// Verify user exists in the system
	u, err := user.Lookup(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Check if user is in an allowed group
	if !isUserInAllowedGroup(username) {
		return nil, ErrUserNotAllowed
	}

	// Authenticate password using pamtester or shadow verification
	if !verifyPassword(username, password) {
		return nil, ErrInvalidCredentials
	}

	// Determine role based on groups
	role := "user"
	for _, adminGroup := range AdminGroups {
		if isUserInGroup(username, adminGroup) {
			role = "admin"
			break
		}
	}

	return &models.User{
		ID:        u.Uid,
		Username:  username,
		Email:     username + "@localhost",
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// authenticateConfig authenticates against config file (fallback)
func authenticateConfig(username, password string) (*models.User, error) {
	cfg := config.Get()

	// Constant-time comparison to prevent timing attacks
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.AdminUser)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(cfg.AdminPassword)) == 1

	if usernameMatch && passwordMatch {
		return &models.User{
			ID:        "admin",
			Username:  cfg.AdminUser,
			Email:     cfg.AdminEmail,
			Role:      "admin",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}

	return nil, ErrInvalidCredentials
}

// isUserInAllowedGroup checks if user belongs to any allowed group
func isUserInAllowedGroup(username string) bool {
	for _, group := range AllowedGroups {
		if isUserInGroup(username, group) {
			return true
		}
	}
	return false
}

// isUserInGroup checks if a user belongs to a specific group
func isUserInGroup(username, groupName string) bool {
	// Special case for root
	if username == "root" && groupName == "root" {
		return true
	}

	// Use the 'groups' command to get user's groups
	cmd := exec.Command("groups", username)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	groups := strings.Fields(string(output))
	// Output format: "username : group1 group2 group3" or just "group1 group2 group3"
	for _, g := range groups {
		if g == groupName {
			return true
		}
	}
	return false
}

// verifyPassword verifies a user's password against the system
func verifyPassword(username, password string) bool {
	// Use Python to verify password against shadow file
	// This is more reliable than using su which doesn't require password when run as root
	script := `
import crypt
import spwd
try:
    shadow = spwd.getspnam('%s')
    if crypt.crypt('%s', shadow.sp_pwdp) == shadow.sp_pwdp:
        print('OK')
    else:
        print('FAIL')
except:
    print('FAIL')
`
	// Escape single quotes in username and password
	safeUsername := strings.ReplaceAll(username, "'", "\\'")
	safePassword := strings.ReplaceAll(password, "'", "\\'")
	
	cmd := exec.Command("python3", "-c", fmt.Sprintf(script, safeUsername, safePassword))
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try using chpasswd --check (available on some systems)
		return verifyPasswordFallback(username, password)
	}

	return strings.TrimSpace(string(output)) == "OK"
}

// verifyPasswordFallback uses an alternative method to verify password
func verifyPasswordFallback(username, password string) bool {
	// Try using login command with expect-like behavior
	// Use timeout to prevent hanging
	script := fmt.Sprintf(`#!/bin/bash
echo '%s' | timeout 5 su -c 'exit 0' %s 2>/dev/null
exit $?
`, strings.ReplaceAll(password, "'", "'\\''"), username)

	cmd := exec.Command("bash", "-c", script)
	err := cmd.Run()
	
	// If running as non-root, su will ask for password
	// If running as root, this won't work - rely on Python method
	return err == nil
}

// GenerateToken generates a JWT token for a user
func GenerateToken(user *models.User) (string, error) {
	cfg := config.Get()

	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "fastcp",
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTSecret))
}

// ValidateToken validates a JWT token and returns the claims
func ValidateToken(tokenString string) (*Claims, error) {
	cfg := config.Get()

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(cfg.JWTSecret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

// GenerateAPIKey generates a new API key
func GenerateAPIKey(name string, userID string, permissions []string) (*models.APIKey, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}

	return &models.APIKey{
		ID:          uuid.New().String(),
		Name:        name,
		Key:         "fcp_" + hex.EncodeToString(keyBytes),
		Permissions: permissions,
		UserID:      userID,
		CreatedAt:   time.Now(),
	}, nil
}

// HashAPIKey creates a hash of an API key for storage
func HashAPIKey(key string) string {
	// In production, use bcrypt or argon2
	// For now, we store keys directly (not recommended for production)
	return key
}

