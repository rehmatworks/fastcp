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

var (
	version    = "0.2.1"
	configPath = flag.String("config", "", "Path to configuration file (default: OS-appropriate path)")
	listenAddr = flag.String("listen", "", "Override listen address (e.g., :8080)")
	devMode    = flag.Bool("dev", false, "Enable development mode")
	noSSL      = flag.Bool("no-ssl", false, "Disable HTTPS and use HTTP instead")
)

func main() {
	flag.Parse()

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

	// Print startup message
	var sslNote string
	if useHTTPS {
		sslNote = "⚠️  Self-signed certificate - accept browser warning"
	} else {
		sslNote = "⚠️  Running in HTTP mode (use --no-ssl=false for HTTPS)"
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
║   Default Login: admin / fastcp2024!                          ║
║                                                               ║
║   %-61s ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝

`, version, protocol, port, cfg.ProxyPort, sslNote)

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
