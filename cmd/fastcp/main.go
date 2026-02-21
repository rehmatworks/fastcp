// FastCP - Lightweight Server Control Panel
// Standalone control panel that manages FrankenPHP/Caddy externally
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/api"
	"github.com/rehmatworks/fastcp/internal/database"
)

//go:embed ui/dist/*
var uiFS embed.FS

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Flags
	listenAddr := flag.String("listen", ":2087", "Listen address")
	dataDir := flag.String("data-dir", "/opt/fastcp/data", "Data directory")
	agentSocket := flag.String("agent-socket", "/var/run/fastcp/agent.sock", "Agent socket path")
	tlsCert := flag.String("tls-cert", "/opt/fastcp/ssl/server.crt", "TLS certificate path")
	tlsKey := flag.String("tls-key", "/opt/fastcp/ssl/server.key", "TLS private key path")
	noTLS := flag.Bool("no-tls", false, "Disable TLS (not recommended)")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FastCP %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	// Setup logging
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		slog.Error("failed to create data directory", "error", err)
		os.Exit(1)
	}

	// Open database
	dbPath := fmt.Sprintf("%s/fastcp.db", *dataDir)
	db, err := database.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Create agent client
	agentClient := agent.NewClient(*agentSocket)

	// Create router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// API routes
	apiHandler := api.NewHandler(db, agentClient, Version)
	r.Route("/api", func(r chi.Router) {
		// Auth
		r.Post("/auth/login", apiHandler.Login)
		r.Post("/auth/logout", apiHandler.Logout)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(apiHandler.AuthMiddleware)

			r.Get("/auth/me", apiHandler.Me)

			// Sites
			r.Get("/sites", apiHandler.ListSites)
			r.Post("/sites", apiHandler.CreateSite)
			r.Get("/sites/{id}", apiHandler.GetSite)
			r.Delete("/sites/{id}", apiHandler.DeleteSite)

			// Site domains
			r.Post("/sites/{id}/domains", apiHandler.AddDomain)
			r.Put("/sites/domains/{domainId}", apiHandler.UpdateDomain)
			r.Post("/sites/domains/{domainId}/set-primary", apiHandler.SetPrimaryDomain)
			r.Delete("/sites/domains/{domainId}", apiHandler.DeleteDomain)

			// Site slug and domain validation
			r.Post("/sites/validate-slug", apiHandler.ValidateSlug)
			r.Post("/sites/generate-slug", apiHandler.GenerateSlug)
			r.Post("/sites/validate-domain", apiHandler.ValidateDomain)

			// Databases
			r.Get("/databases", apiHandler.ListDatabases)
			r.Post("/databases", apiHandler.CreateDatabase)
			r.Delete("/databases/{id}", apiHandler.DeleteDatabase)
			r.Get("/databases/{id}/phpmyadmin", apiHandler.PhpMyAdminSignon)

			// SSH Keys
			r.Get("/ssh-keys", apiHandler.ListSSHKeys)
			r.Post("/ssh-keys", apiHandler.AddSSHKey)
			r.Delete("/ssh-keys/{id}", apiHandler.RemoveSSHKey)

			// Cron Jobs
			r.Get("/cron", apiHandler.ListCronJobs)
			r.Post("/cron", apiHandler.CreateCronJob)
			r.Put("/cron/{id}", apiHandler.UpdateCronJob)
			r.Post("/cron/{id}/toggle", apiHandler.ToggleCronJob)
			r.Delete("/cron/{id}", apiHandler.DeleteCronJob)
			r.Post("/cron/validate", apiHandler.ValidateCronExpression)

			// System
			r.Get("/system/status", apiHandler.SystemStatus)
			r.Get("/system/services", apiHandler.SystemServices)

			// Admin-only routes
			r.Group(func(r chi.Router) {
				r.Use(apiHandler.AdminMiddleware)

				// User management
				r.Get("/users", apiHandler.ListUsers)
				r.Post("/users", apiHandler.CreateUser)
				r.Delete("/users/{username}", apiHandler.DeleteUser)
				r.Post("/users/{username}/toggle-suspend", apiHandler.ToggleUserSuspension)

				// System updates
				r.Get("/system/check-update", apiHandler.CheckUpdate)
				r.Post("/system/update", apiHandler.PerformUpdate)
			})
		})
	})

	// phpMyAdmin reverse proxy
	phpMyAdminURL, _ := url.Parse("http://localhost:8088")
	phpMyAdminProxy := httputil.NewSingleHostReverseProxy(phpMyAdminURL)
	r.HandleFunc("/phpmyadmin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/phpmyadmin/", http.StatusMovedPermanently)
	})
	r.HandleFunc("/phpmyadmin/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/phpmyadmin")
		
		// Allow signon.php with token (token is the authentication)
		// Also allow access if user has valid FastCP session (for subsequent requests after signon)
		isSignonWithToken := strings.HasPrefix(path, "/signon.php") && req.URL.Query().Get("token") != ""
		hasPhpMyAdminSession := false
		for _, cookie := range req.Cookies() {
			if cookie.Name == "SignonSession" || cookie.Name == "phpMyAdmin" {
				hasPhpMyAdminSession = true
				break
			}
		}
		
		if !isSignonWithToken && !hasPhpMyAdminSession && !apiHandler.IsAuthenticated(req) {
			http.Redirect(w, req, "/?redirect=phpmyadmin", http.StatusFound)
			return
		}

		// Strip /phpmyadmin prefix for the backend
		req.URL.Path = path
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Host = "localhost"
		phpMyAdminProxy.ServeHTTP(w, req)
	})

	// Serve embedded UI
	uiSubFS, err := fs.Sub(uiFS, "ui/dist")
	if err != nil {
		slog.Error("failed to get UI filesystem", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(uiSubFS))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		if _, err := fs.Stat(uiSubFS, path[1:]); err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/index.html"
		}

		fileServer.ServeHTTP(w, r)
	})

	// Create server
	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		slog.Info("FastCP starting", "listen", *listenAddr, "version", Version)

		if *noTLS {
			slog.Warn("TLS disabled - connections are not encrypted!")
			slog.Info("Open http://localhost" + *listenAddr + " in your browser")
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("Open https://localhost" + *listenAddr + " in your browser")
			if err := server.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && err != http.ErrServerClosed {
				slog.Error("TLS server error", "error", err)
				slog.Info("Hint: Generate certificates with: openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout server.key -out server.crt")
				os.Exit(1)
			}
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
