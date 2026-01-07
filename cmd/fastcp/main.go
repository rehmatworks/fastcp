// Package main is the entry point for the FastCP control panel application.
//
// FastCP is a modern PHP hosting control panel built with Go and React.
// It provides a web-based interface for managing PHP websites, databases,
// and system users on Linux servers.
//
// # Security Considerations
//
// This application runs with elevated privileges and manages system resources.
// Important security practices:
//
//   - Always change the default admin password after installation
//   - Configure a unique JWT secret in the configuration file
//   - Use HTTPS in production (enabled by default)
//   - Restrict network access to the admin panel port
//   - Regularly update to the latest version for security patches
//
// # Configuration
//
// The application reads its configuration from a JSON file. The default
// location depends on the runtime mode:
//
//   - Production: /etc/fastcp/config.json
//   - Development: ./.fastcp/config.json
//
// A default configuration file is created on first run if none exists.
//
// # Usage
//
// Run 'fastcp -help' for available command-line options.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rehmatworks/fastcp/internal/api"
	"github.com/rehmatworks/fastcp/internal/caddy"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/jail"
	"github.com/rehmatworks/fastcp/internal/php"
	"github.com/rehmatworks/fastcp/internal/sites"
	"github.com/rehmatworks/fastcp/internal/ssl"
	"github.com/rehmatworks/fastcp/internal/upgrade"
)

// Application version - updated during release process
var version = "0.2.6"

// Command-line flags for runtime configuration
var (
	// configPath specifies the path to the JSON configuration file.
	// If not provided, the application uses OS-appropriate defaults:
	//   - Linux (production): /etc/fastcp/config.json
	//   - Development mode:   ./.fastcp/config.json
	configPath = flag.String("config", "", "Path to configuration file")

	// listenAddr overrides the default listen address from config.
	// Format: [host]:port (e.g., ":8080", "127.0.0.1:8080", "0.0.0.0:443")
	// Warning: Binding to 0.0.0.0 exposes the panel to all network interfaces.
	listenAddr = flag.String("listen", "", "Override listen address (e.g., :8080)")

	// devMode enables development mode with relaxed security settings.
	// In development mode:
	//   - Uses local ./.fastcp/ directory for all data
	//   - Enables verbose debug logging
	//   - Disables HTTPS by default
	// WARNING: Never use development mode in production environments.
	devMode = flag.Bool("dev", false, "Enable development mode (NOT for production)")

	// noSSL disables HTTPS and runs the admin panel over HTTP.
	// WARNING: This exposes credentials and session tokens in plaintext.
	// Only use this behind a reverse proxy that handles TLS termination.
	noSSL = flag.Bool("no-ssl", false, "Disable HTTPS (use only behind TLS-terminating proxy)")

	// showVersion prints the version number and exits.
	showVersion = flag.Bool("version", false, "Print version information and exit")

	// showHelp displays usage information.
	showHelp = flag.Bool("help", false, "Show this help message")
)

// printUsage displays detailed usage information with security notes.
func printUsage() {
	fmt.Printf(`FastCP - Modern PHP Hosting Control Panel (v%s)

USAGE:
    fastcp [OPTIONS]

OPTIONS:
    -config string
        Path to configuration file.
        Default: /etc/fastcp/config.json (production)
                 ./.fastcp/config.json (development)

    -listen string
        Override the listen address for the admin panel.
        Examples: ":8080", "127.0.0.1:8080", "0.0.0.0:443"

    -dev
        Enable development mode. Uses local directories and
        enables debug logging. NOT FOR PRODUCTION USE.

    -no-ssl
        Disable HTTPS for the admin panel. Only use this when
        running behind a reverse proxy that handles TLS.

    -version
        Print version information and exit.

    -help
        Show this help message.

SECURITY NOTES:
    * Change the default admin password immediately after installation
    * Change the JWT secret in the configuration file
    * Use HTTPS in production (enabled by default)
    * Restrict network access to the admin panel port

CONFIGURATION:
    On first run, a default configuration file is created.
    Edit this file to customize settings before starting the server.

DOCUMENTATION:
    https://github.com/rehmatworks/fastcp

`, version)
}

func main() {
	flag.Parse()

	// Handle --version flag
	if *showVersion {
		fmt.Printf("FastCP version %s\n", version)
		os.Exit(0)
	}

	// Handle --help flag
	if *showHelp {
		printUsage()
		os.Exit(0)
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if *devMode {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	if *devMode {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		}))
	}

	slog.SetDefault(logger)
	logger.Info("Starting FastCP", "version", version)

	// Load configuration
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	logger.Info("Using config", "path", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("Data directories", "data", cfg.DataDir, "sites", cfg.SitesDir, "logs", cfg.LogDir)

	// Override listen address if provided
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}

	// Initialize site manager
	siteManager := sites.NewManager(cfg.DataDir)
	if err := siteManager.Load(); err != nil {
		logger.Error("Failed to load sites", "error", err)
		os.Exit(1)
	}

	// Secure the base sites directory
	sites.SecureBaseDirectory(cfg.SitesDir)

	// Setup SSH jail for SFTP-only users
	if !config.IsDevMode() {
		if err := jail.SetupJailGroup(); err != nil {
			logger.Warn("Failed to setup jail group", "error", err)
		}
		if err := jail.SetupSSHConfig(); err != nil {
			logger.Warn("Failed to setup SSH jail config", "error", err)
		} else {
			logger.Info("SSH jail configuration verified")
		}
	}

	// Initialize Caddy generator
	// Use templates from config directory (same parent as config file)
	configDir := filepath.Dir(config.DefaultConfigPath())
	caddyGen := caddy.NewGenerator(
		filepath.Join(configDir, "templates"),
		cfg.DataDir+"/caddy",
	)

	// Ensure fastcp user exists for running PHP securely
	if err := php.EnsurePHPUser(); err != nil {
		logger.Warn("Failed to create fastcp user, PHP will run as current user", "error", err)
	}

	// Initialize PHP manager
	phpManager := php.NewManager(caddyGen, siteManager.GetAll)
	if err := phpManager.Initialize(); err != nil {
		logger.Error("Failed to initialize PHP manager", "error", err)
		os.Exit(1)
	}

	// Ensure PHP binaries are downloaded
	logger.Info("Checking PHP binaries...")
	downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	if err := phpManager.EnsureBinaries(downloadCtx, logger); err != nil {
		logger.Error("Failed to ensure PHP binaries", "error", err)
		// Don't exit - allow API to run even if download fails
		// User can download via admin panel
	}
	downloadCancel()

	// Start PHP instances and proxy
	logger.Info("Starting PHP instances and proxy...")
	if err := phpManager.StartAll(); err != nil {
		logger.Error("Failed to start PHP instances", "error", err)
		// Don't exit - allow API to run even if PHP fails to start
		// This allows the user to diagnose/fix via the admin panel
	} else {
		logger.Info("PHP instances and proxy started successfully")
	}

	// Initialize per-user PHP manager
	userPHPManager := php.NewUserPHPManager()
	// Recover any existing user PHP instances from PID files
	if err := userPHPManager.RecoverInstances(); err != nil {
		logger.Warn("Failed to recover user PHP instances", "error", err)
	}
	logger.Info("User PHP manager initialized")

	// Initialize database manager
	dbManager := database.NewManager()
	logger.Info("Database manager initialized")

	// Initialize upgrade manager
	upgradeManager := upgrade.NewManager(version, cfg.DataDir)
	if upgradeManager.CheckLockFile() {
		logger.Warn("Upgrade lock file found - previous upgrade may have been interrupted")
		upgradeManager.CleanupLockFile()
	}
	logger.Info("Upgrade manager initialized", "version", version)

	// Create API server
	apiServer := api.NewServer(
		siteManager,
		phpManager,
		userPHPManager,
		dbManager,
		caddyGen,
		upgradeManager,
		logger,
	)

	// Setup HTTP server
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      apiServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Determine if we should use HTTPS
	useHTTPS := !*devMode && !*noSSL

	// Initialize SSL if needed
	var sslManager *ssl.Manager
	var protocol string
	if useHTTPS {
		sslManager = ssl.NewManager(cfg.DataDir)
		if err := sslManager.EnsureCertificate(); err != nil {
			logger.Error("Failed to generate SSL certificate", "error", err)
			logger.Warn("Falling back to HTTP")
			useHTTPS = false
			protocol = "http"
		} else {
			logger.Info("SSL certificate ready")
			protocol = "https"
		}
	} else {
		protocol = "http"
	}

	// Start server in goroutine
	go func() {
		if useHTTPS {
			certPath, keyPath := sslManager.CertPaths()
			logger.Info("FastCP API server starting with HTTPS", "address", cfg.ListenAddr)
			if err := server.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
				logger.Error("Server error", "error", err)
				os.Exit(1)
			}
		} else {
			logger.Info("FastCP API server starting with HTTP", "address", cfg.ListenAddr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("Server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	// Extract port from listen address for display
	port := cfg.ListenAddr
	if strings.HasPrefix(port, ":") {
		port = port[1:]
	}

	// Print startup message with security-conscious information
	var sslNote string
	if useHTTPS {
		sslNote = "TLS enabled (self-signed certificate)"
	} else {
		sslNote = "WARNING: TLS disabled - use only behind reverse proxy"
	}

	fmt.Printf(`
╔═══════════════════════════════════════════════════════════════╗
║                                                               ║
║   ███████╗ █████╗ ███████╗████████╗ ██████╗██████╗            ║
║   ██╔════╝██╔══██╗██╔════╝╚══██╔══╝██╔════╝██╔══██╗           ║
║   █████╗  ███████║███████╗   ██║   ██║     ██████╔╝           ║
║   ██╔══╝  ██╔══██║╚════██║   ██║   ██║     ██╔═══╝            ║
║   ██║     ██║  ██║███████║   ██║   ╚██████╗██║                ║
║   ╚═╝     ╚═╝  ╚═╝╚══════╝   ╚═╝    ╚═════╝╚═╝                ║
║                                                               ║
║   Modern PHP Hosting Control Panel                            ║
║   Version: %-51s ║
║                                                               ║
║   Admin Panel: %s://localhost:%-27s ║
║   Sites Proxy: http://localhost:%-28d ║
║   Status:      %-47s ║
║                                                               ║
║   Run 'fastcp -help' for configuration options                ║
║   Documentation: https://github.com/rehmatworks/fastcp        ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
`, version, protocol, port, cfg.ProxyPort, sslNote)

	// Display generated credentials for new installations or upgrades
	// This is shown ONCE and must be saved by the administrator
	if creds := config.GetGeneratedCredentials(); creds != nil {
		if creds.IsNewInstall {
			fmt.Printf(`
╔═══════════════════════════════════════════════════════════════╗
║                    NEW INSTALLATION DETECTED                  ║
╠═══════════════════════════════════════════════════════════════╣
║                                                               ║
║   Secure credentials have been generated for this             ║
║   installation. SAVE THESE NOW - they will not be shown       ║
║   again after restart!                                        ║
║                                                               ║
║   Username: admin                                             ║
║   Password: %-51s ║
║                                                               ║
║   Config:   %-51s ║
║                                                               ║
║   ⚠️  Change these credentials after first login!              ║
║   ⚠️  Store the password in a secure password manager!         ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
`, creds.AdminPassword, cfgPath)
		} else {
			// Upgrade from insecure defaults
			fmt.Printf(`
╔═══════════════════════════════════════════════════════════════╗
║                   SECURITY UPGRADE APPLIED                    ║
╠═══════════════════════════════════════════════════════════════╣
║                                                               ║
║   Insecure default credentials were detected and have been    ║
║   replaced with secure random values.                         ║
║                                                               ║
║   New Password: %-46s ║
║                                                               ║
║   The configuration file has been updated automatically.      ║
║   SAVE THIS PASSWORD NOW - it will not be shown again!        ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
`, creds.AdminPassword)
		}
		logger.Warn("Generated credentials displayed - ensure they are saved securely")
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down FastCP...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "error", err)
	}

	// Stop PHP instances
	if err := phpManager.StopAll(); err != nil {
		logger.Error("Failed to stop PHP instances", "error", err)
	}

	logger.Info("FastCP stopped")
}
