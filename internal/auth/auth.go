// Package auth provides authentication and authorization functionality for FastCP.
//
// This package supports multiple authentication methods:
//   - Unix/PAM authentication against system users (Linux only)
//   - Configuration-based authentication (fallback for development)
//
// Security considerations:
//   - All password verification uses secure methods that avoid shell injection
//   - JWT tokens are signed with HMAC-SHA256
//   - Constant-time comparison is used where appropriate to prevent timing attacks
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

// usernameRegex validates usernames to prevent injection attacks.
// Only allows alphanumeric characters, underscores, and hyphens.
// Must start with a letter or underscore, max 32 characters.
var usernameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrAPIKeyNotFound     = errors.New("api key not found")
	ErrAPIKeyExpired      = errors.New("api key expired")
	ErrUserNotAllowed     = errors.New("user not allowed to access FastCP")
	ErrInvalidUsername    = errors.New("invalid username format")
)

// isValidUsername checks if a username is safe for use in system commands.
// This is a critical security function that prevents command injection.
func isValidUsername(username string) bool {
	if username == "" || len(username) > 32 {
		return false
	}
	return usernameRegex.MatchString(username)
}

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
	// SECURITY: Validate username format to prevent injection attacks
	// This must be done BEFORE any system calls
	if !isValidUsername(username) {
		return nil, ErrInvalidCredentials
	}

	// Verify user exists in the system
	u, err := user.Lookup(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Check if user is in an allowed group
	if !isUserInAllowedGroup(username) {
		return nil, ErrUserNotAllowed
	}

	// Authenticate password using secure verification methods
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

// verifyPassword verifies a user's password against the system shadow file.
//
// SECURITY: This function uses stdin to pass the password to Python,
// avoiding command injection vulnerabilities. The username is validated
// before this function is called, but we validate again for defense in depth.
//
// The verification process:
// 1. Reads the user's password hash from /etc/shadow via Python's spwd module
// 2. Hashes the provided password with the same salt
// 3. Compares the hashes in constant time
func verifyPassword(username, password string) bool {
	// Defense in depth: validate username even though caller should have done it
	if !isValidUsername(username) {
		return false
	}

	// Python script that reads credentials from stdin (line 1: username, line 2: password)
	// This prevents command injection as no user input is embedded in the script
	const script = `
import sys
import crypt
import spwd

def verify():
    try:
        # Read username and password from stdin (safe from injection)
        username = sys.stdin.readline().rstrip('\n')
        password = sys.stdin.readline().rstrip('\n')

        if not username:
            return False

        # Get shadow entry for user
        shadow = spwd.getspnam(username)

        # Hash the provided password with the stored salt and compare
        computed = crypt.crypt(password, shadow.sp_pwdp)

        # Use constant-time comparison to prevent timing attacks
        if len(computed) != len(shadow.sp_pwdp):
            return False
        result = 0
        for x, y in zip(computed.encode(), shadow.sp_pwdp.encode()):
            result |= x ^ y
        return result == 0
    except (KeyError, PermissionError, Exception):
        return False

if verify():
    print('OK')
else:
    print('FAIL')
`

	cmd := exec.Command("python3", "-c", script)

	// Pass credentials via stdin - safe from injection
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return verifyPasswordFallback(username, password)
	}

	// Start the command before writing to stdin
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return verifyPasswordFallback(username, password)
	}

	// Write credentials to stdin (username on line 1, password on line 2)
	io.WriteString(stdin, username+"\n")
	io.WriteString(stdin, password+"\n")
	stdin.Close()

	// Read output
	output, err := cmd.Output()
	if err != nil {
		// Command failed, try fallback method
		return verifyPasswordFallback(username, password)
	}

	return strings.TrimSpace(string(output)) == "OK"
}

// verifyPasswordFallback uses the 'su' command to verify password.
//
// SECURITY: This function passes the password via stdin to 'su',
// avoiding command injection. The username is passed as a command
// argument (not via shell interpolation) and is pre-validated.
//
// This fallback is used when:
// - Python is not available
// - The spwd module fails (e.g., insufficient permissions)
// - Shadow file is not readable
func verifyPasswordFallback(username, password string) bool {
	// Defense in depth: validate username
	if !isValidUsername(username) {
		return false
	}

	// Use 'su' with the username as a direct argument (not shell-interpolated)
	// The -c flag runs a command as the target user
	// We use 'true' as the command - if auth succeeds, it exits 0
	cmd := exec.Command("su", "-c", "true", username)

	// Pass password via stdin - safe from injection
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return false
	}

	// Write password to stdin
	io.WriteString(stdin, password+"\n")
	stdin.Close()

	// Wait for command to complete with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 5 second timeout to prevent hanging
	select {
	case err := <-done:
		return err == nil
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		return false
	}
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
