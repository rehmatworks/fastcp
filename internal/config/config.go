package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rehmatworks/fastcp/internal/models"
)

var (
	cfg     *models.Config
	cfgOnce sync.Once
	cfgMu   sync.RWMutex
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
		sitesDir = getEnvOrDefault(EnvSitesDir, "/var/www")
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

// DefaultConfig returns the default configuration
func DefaultConfig() *models.Config {
	dataDir, sitesDir, logDir, _, binaryPath, proxyPort, proxySSLPort := getDefaultPaths()

	return &models.Config{
		AdminUser:     "admin",
		AdminPassword: "fastcp2024!", // Default password - should be changed
		AdminEmail:    "support@fastcp.org",
		JWTSecret:     "change-this-secret-in-production-please",
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

// Load loads configuration from file or creates default
func Load(configPath string) (*models.Config, error) {
	cfgOnce.Do(func() {
		cfg = DefaultConfig()

		if configPath == "" {
			configPath = DefaultConfigPath()
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Create default config file
				_ = Save(configPath)
				return
			}
			return
		}

		if err := json.Unmarshal(data, cfg); err != nil {
			return
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

