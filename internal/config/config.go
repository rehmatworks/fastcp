// Package config handles application configuration management.
//
// Configuration is stored in a JSON file and includes settings for
// authentication, server addresses, PHP versions, and directory paths.
//
// On first run, secure random credentials are generated automatically
// to prevent the use of default passwords in production environments.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rehmatworks/fastcp/internal/models"
)

// GeneratedCredentials holds the credentials generated during first installation.
// These are displayed to the user once and should be saved securely.
type GeneratedCredentials struct {
	AdminPassword string // The generated admin password
	JWTSecret     string // The generated JWT signing secret
	IsNewInstall  bool   // True if this is a fresh installation
}

var (
	cfg     *models.Config
	cfgOnce sync.Once
	cfgMu   sync.RWMutex

	// generatedCreds stores credentials generated during first installation.
	// This is only populated when a new config file is created.
	generatedCreds *GeneratedCredentials
)

// Environment variable names
const (
	EnvDevMode      = "FASTCP_DEV"        // Set to "1" for development mode
	EnvDataDir      = "FASTCP_DATA_DIR"   // Override data directory
	EnvSitesDir     = "FASTCP_SITES_DIR"  // Override sites directory
	EnvLogDir       = "FASTCP_LOG_DIR"    // Override log directory
	EnvConfigDir    = "FASTCP_CONFIG_DIR" // Override config directory
	EnvBinaryPath   = "FASTCP_BINARY"     // Override FrankenPHP binary path
	EnvProxyPort    = "FASTCP_PORT"       // Override proxy HTTP port
	EnvProxySSLPort = "FASTCP_SSL_PORT"   // Override proxy HTTPS port
	EnvListenAddr   = "FASTCP_LISTEN"     // Override admin panel listen address
)

// IsDevMode returns true if running in development mode
func IsDevMode() bool {
	return os.Getenv(EnvDevMode) == "1"
}

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvIntOrDefault returns environment variable as int or default
func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// generateSecurePassword creates a cryptographically secure random password.
// The password contains a mix of uppercase, lowercase, numbers, and special characters.
// Length should be at least 16 characters for adequate security.
func generateSecurePassword(length int) string {
	// Character set excluding ambiguous characters (0, O, l, 1, I)
	const charset = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789!@#$%^&*"

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less secure but still random method
		// This should never happen in practice
		return "ChangeMe!" + base64.StdEncoding.EncodeToString(bytes)[:length-9]
	}

	for i := range bytes {
		bytes[i] = charset[int(bytes[i])%len(charset)]
	}
	return string(bytes)
}

// generateSecureSecret creates a cryptographically secure random string for JWT signing.
// Returns a base64-encoded string suitable for use as a secret key.
// The resulting secret has high entropy (256 bits) for security.
func generateSecureSecret(byteLength int) string {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback - this should never happen
		return "insecure-fallback-change-immediately-" + generateSecurePassword(32)
	}
	return base64.URLEncoding.EncodeToString(bytes)
}

// GetGeneratedCredentials returns the credentials that were generated during
// first installation. Returns nil if this is not a new installation or if
// the config was loaded from an existing file.
//
// IMPORTANT: These credentials are only available once. After the application
// restarts, this function will return nil. Users must save these credentials
// securely during initial setup.
func GetGeneratedCredentials() *GeneratedCredentials {
	return generatedCreds
}

// getDefaultPaths returns paths based on environment and mode
func getDefaultPaths() (dataDir, sitesDir, logDir, configDir, binaryPath string, proxyPort, proxySSLPort int) {
	if IsDevMode() {
		// Development mode: use local directories relative to current working directory
		cwd, _ := os.Getwd()
		dataDir = getEnvOrDefault(EnvDataDir, filepath.Join(cwd, ".fastcp", "data"))
		sitesDir = getEnvOrDefault(EnvSitesDir, filepath.Join(cwd, ".fastcp", "sites"))
		logDir = getEnvOrDefault(EnvLogDir, filepath.Join(cwd, ".fastcp", "logs"))
		configDir = getEnvOrDefault(EnvConfigDir, filepath.Join(cwd, ".fastcp"))
		binaryPath = getEnvOrDefault(EnvBinaryPath, filepath.Join(cwd, ".fastcp", "bin", "frankenphp"))
		proxyPort = getEnvIntOrDefault(EnvProxyPort, 8000)       // Non-privileged port
		proxySSLPort = getEnvIntOrDefault(EnvProxySSLPort, 8443) // Non-privileged HTTPS port
	} else {
		// Production mode: use standard Linux system paths
		dataDir = getEnvOrDefault(EnvDataDir, "/var/lib/fastcp")
		sitesDir = getEnvOrDefault(EnvSitesDir, "/home") // Sites are at /home/{user}/www/{domain}
		logDir = getEnvOrDefault(EnvLogDir, "/var/log/fastcp")
		configDir = getEnvOrDefault(EnvConfigDir, "/etc/fastcp")
		binaryPath = getEnvOrDefault(EnvBinaryPath, "/usr/local/bin/frankenphp")
		proxyPort = getEnvIntOrDefault(EnvProxyPort, 80)
		proxySSLPort = getEnvIntOrDefault(EnvProxySSLPort, 443)
	}
	return
}

// DefaultConfigPath returns the default config path based on mode
func DefaultConfigPath() string {
	_, _, _, configDir, _, _, _ := getDefaultPaths()
	return filepath.Join(configDir, "config.json")
}

// DefaultConfig returns the default configuration with secure generated credentials.
// Each new installation gets unique, cryptographically secure passwords and secrets.
func DefaultConfig() *models.Config {
	dataDir, sitesDir, logDir, _, binaryPath, proxyPort, proxySSLPort := getDefaultPaths()

	// Generate secure credentials for new installations
	// Password: 20 characters with mixed case, numbers, and symbols
	// JWT Secret: 32 bytes (256 bits) of entropy, base64 encoded
	adminPassword := generateSecurePassword(20)
	jwtSecret := generateSecureSecret(32)

	return &models.Config{
		AdminUser:     "admin",
		AdminPassword: adminPassword,
		AdminEmail:    "admin@localhost",
		JWTSecret:     jwtSecret,
		DataDir:       dataDir,
		SitesDir:      sitesDir,
		LogDir:        logDir,
		ListenAddr:    ":8080",
		ProxyPort:     proxyPort,
		ProxySSLPort:  proxySSLPort,
		PHPVersions: []models.PHPVersionConfig{
			{
				Version:    "8.4",
				Port:       9084,
				AdminPort:  2084,
				BinaryPath: binaryPath,
				Enabled:    true,
				NumThreads: 0, // auto
				MaxThreads: 0, // auto
			},
			{
				Version:    "8.3",
				Port:       9083,
				AdminPort:  2083,
				BinaryPath: binaryPath,
				Enabled:    false, // Disabled by default - enable if you have multiple PHP versions
				NumThreads: 0,
				MaxThreads: 0,
			},
			{
				Version:    "8.2",
				Port:       9082,
				AdminPort:  2082,
				BinaryPath: binaryPath,
				Enabled:    false, // Disabled by default - enable if you have multiple PHP versions
				NumThreads: 0,
				MaxThreads: 0,
			},
		},
	}
}

// Load loads configuration from file or creates a new one with secure credentials.
//
// On first run (when no config file exists), this function:
//  1. Generates a secure random admin password
//  2. Generates a secure random JWT signing secret
//  3. Creates the config file with these credentials
//  4. Stores the credentials for one-time display to the user
//
// The generated credentials can be retrieved via GetGeneratedCredentials().
func Load(configPath string) (*models.Config, error) {
	cfgOnce.Do(func() {
		cfg = DefaultConfig()

		if configPath == "" {
			configPath = DefaultConfigPath()
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				// New installation - store generated credentials for display
				generatedCreds = &GeneratedCredentials{
					AdminPassword: cfg.AdminPassword,
					JWTSecret:     cfg.JWTSecret,
					IsNewInstall:  true,
				}
				// Create config file with secure credentials
				_ = Save(configPath)
				return
			}
			return
		}

		// Existing config file - parse it
		if err := json.Unmarshal(data, cfg); err != nil {
			return
		}

		// Check if the config has insecure default values that need updating
		// This handles upgrades from older versions with hardcoded defaults
		needsSave := false
		if cfg.JWTSecret == "change-this-secret-in-production-please" {
			cfg.JWTSecret = generateSecureSecret(32)
			needsSave = true
		}
		if cfg.AdminPassword == "fastcp2024!" {
			newPassword := generateSecurePassword(20)
			cfg.AdminPassword = newPassword
			generatedCreds = &GeneratedCredentials{
				AdminPassword: newPassword,
				JWTSecret:     cfg.JWTSecret,
				IsNewInstall:  false, // This is an upgrade, not fresh install
			}
			needsSave = true
		}
		if needsSave {
			_ = Save(configPath)
		}
	})

	return cfg, nil
}

// Get returns the current configuration
func Get() *models.Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

// Save saves configuration to file
func Save(configPath string) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	if configPath == "" {
		configPath = DefaultConfigPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// Update updates the configuration
func Update(newCfg *models.Config) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	cfg = newCfg
}
