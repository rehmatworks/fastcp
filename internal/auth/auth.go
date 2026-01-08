package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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

// APIKeyValidator is a function type for validating API keys
type APIKeyValidator func(key string) (*models.APIKey, error)

// Global API key validator - set by the main application
var apiKeyValidator APIKeyValidator

// SetAPIKeyValidator sets the global API key validator function
func SetAPIKeyValidator(validator APIKeyValidator) {
	apiKeyValidator = validator
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
// Uses Unix/PAM authentication on Linux systems
func Authenticate(username, password string) (*models.User, error) {
	// Development fallback: if running in dev mode and an admin username/password
	// is configured in `.fastcp/config.json`, allow login with those credentials.
	// This is intended for local development only and requires FASTCP_DEV=1.
	if config.IsDevMode() {
		cfg := config.Get()
		// Only allow explicitly when configured for dev mode
		if cfg != nil && cfg.AllowAdminPasswordLogin && cfg.AdminUser != "" && cfg.AdminPassword != "" {
			if username == cfg.AdminUser && password == cfg.AdminPassword {
				return &models.User{
					ID:        "admin",
					Username:  username,
					Email:     username + "@localhost",
					Role:      "admin",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}, nil
			}
		}
	}

	// Use Unix authentication (Linux only)
	if runtime.GOOS == "linux" {
		return authenticateUnix(username, password)
	}

	// For non-Linux systems (development), return error
	return nil, ErrInvalidCredentials
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

	// Authenticate password using PAM/direct shadow verification
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

// Password verification is platform-specific. Implementations live in:
//   - verify_password_linux.go (uses PAM on Linux)
//   - verify_password_nonlinux.go (fallback using shadow file / scripts on other platforms)
// The actual `verifyPassword` function is defined in those files.

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

// ValidateAPIKey validates an API key against stored keys
func ValidateAPIKey(key string) (*models.APIKey, error) {
	if apiKeyValidator != nil {
		return apiKeyValidator(key)
	}

	// Fallback: basic validation
	if !strings.HasPrefix(key, "fcp_") {
		return nil, ErrAPIKeyNotFound
	}

	return &models.APIKey{
		Key:         key,
		Permissions: []string{"sites:read", "sites:write"}, // Default permissions
	}, nil
}
