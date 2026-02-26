// FastCP - Lightweight Server Control Panel
// Standalone control panel that manages Caddy/PHP-FPM externally
package main

import (
	"context"
	cryptorand "crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/api"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

type pmaCredentials struct {
	User     string
	Password string
	DB       string
	Username string // system username -- determines which socket to proxy to
	Expiry   time.Time
}

var pmaStore sync.Map

//go:embed ui/dist/*
var uiFS embed.FS

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Flags
	listenAddr := flag.String("listen", ":2050", "Listen address")
	dataDir := flag.String("data-dir", "/opt/fastcp/data", "Data directory")
	agentSocket := flag.String("agent-socket", "/opt/fastcp/run/agent.sock", "Agent socket path")
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
	workersCtx, workersCancel := context.WithCancel(context.Background())
	defer workersCancel()
	apiHandler.StartBackgroundWorkers(workersCtx)
	r.Route("/api", func(r chi.Router) {
		// Auth
		r.Post("/auth/login", apiHandler.Login)
		r.Post("/auth/logout", apiHandler.Logout)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(apiHandler.AuthMiddleware)

			r.Get("/auth/me", apiHandler.Me)
			r.Post("/auth/impersonation/stop", apiHandler.StopImpersonation)

			// Sites
			r.Get("/sites", apiHandler.ListSites)
			r.Post("/sites", apiHandler.CreateSite)
			r.Get("/sites/{id}", apiHandler.GetSite)
			r.Put("/sites/{id}/settings", apiHandler.UpdateSiteSettings)
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
			r.Post("/databases/{id}/reset-password", apiHandler.ResetDatabasePassword)
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

			// Backups
			r.Get("/backups/config", apiHandler.GetBackupConfig)
			r.Put("/backups/config", apiHandler.SaveBackupConfig)
			r.Post("/backups/config/test", apiHandler.TestBackupConfig)
			r.Get("/backups/rclone/status", apiHandler.GetBackupRcloneStatus)
			r.Post("/backups/rclone/install", apiHandler.InstallBackupRclone)
			r.Post("/backups/run", apiHandler.RunBackupNow)
			r.Get("/backups/jobs", apiHandler.ListBackupJobs)
			r.Get("/backups/snapshots", apiHandler.ListBackupSnapshots)
			r.Post("/backups/snapshots/delete", apiHandler.DeleteBackupSnapshot)
			r.Get("/backups/snapshots/{snapshotId}/download", apiHandler.DownloadBackupSnapshot)
			r.Post("/backups/restore/site", apiHandler.RestoreSite)
			r.Post("/backups/restore/database", apiHandler.RestoreDatabase)

			// System
			r.Get("/system/status", apiHandler.SystemStatus)
			r.Get("/system/services", apiHandler.SystemServices)

			// Admin-only routes
			r.Group(func(r chi.Router) {
				r.Use(apiHandler.AdminMiddleware)

				// User management
				r.Get("/users", apiHandler.ListUsers)
				r.Post("/users", apiHandler.CreateUser)
				r.Post("/admin/impersonate", apiHandler.StartImpersonation)
				r.Delete("/users/{username}", apiHandler.DeleteUser)
				r.Post("/users/{username}/toggle-suspend", apiHandler.ToggleUserSuspension)
				r.Put("/users/{username}/resources", apiHandler.UpdateUserResources)

				// System updates
				r.Get("/system/check-update", apiHandler.CheckUpdate)
				r.Post("/system/update", apiHandler.PerformUpdate)

				// MySQL config
				r.Get("/system/mysql-config", apiHandler.GetMySQLConfig)
				r.Put("/system/mysql-config", apiHandler.SetMySQLConfig)
				r.Get("/system/ssh-config", apiHandler.GetSSHConfig)
				r.Put("/system/ssh-config", apiHandler.SetSSHConfig)
				r.Get("/system/php-default-config", apiHandler.GetPHPDefaultConfig)
				r.Put("/system/php-default-config", apiHandler.SetPHPDefaultConfig)
				r.Post("/system/php/install-version", apiHandler.InstallPHPVersion)
				r.Get("/system/php/install-version-status", apiHandler.GetPHPVersionInstallStatus)
				r.Get("/system/caddy-config", apiHandler.GetCaddyConfig)
				r.Put("/system/caddy-config", apiHandler.SetCaddyConfig)
				r.Get("/system/firewall", apiHandler.GetFirewallStatus)
				r.Post("/system/firewall/install", apiHandler.InstallFirewall)
				r.Put("/system/firewall/enabled", apiHandler.SetFirewallEnabled)
				r.Post("/system/firewall/allow", apiHandler.FirewallAllowPort)
				r.Post("/system/firewall/deny", apiHandler.FirewallDenyPort)
				r.Post("/system/firewall/delete", apiHandler.FirewallDeleteRule)
			})
		})
	})

	// phpMyAdmin reverse proxy - uses FastCP-managed credentials + shared FPM backend
	r.HandleFunc("/phpmyadmin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/phpmyadmin/", http.StatusMovedPermanently)
	})
	r.HandleFunc("/phpmyadmin/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/phpmyadmin")

		// Token present: decrypt, store credentials, set cookie, redirect
		if token := req.URL.Query().Get("fastcp_token"); token != "" {
			payload, err := crypto.DecryptURLSafe(token)
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusForbidden)
				return
			}

			parts := strings.SplitN(payload, "|", 5)
			if len(parts) != 5 {
				http.Error(w, "Invalid token format", http.StatusForbidden)
				return
			}

			expiry, _ := strconv.ParseInt(parts[4], 10, 64)
			if time.Now().Unix() > expiry {
				http.Error(w, "Token expired", http.StatusForbidden)
				return
			}

			buf := make([]byte, 32)
			cryptorand.Read(buf)
			sessionID := hex.EncodeToString(buf)

			pmaStore.Store(sessionID, &pmaCredentials{
				User:     parts[0],
				Password: parts[1],
				DB:       parts[2],
				Username: parts[3],
				Expiry:   time.Now().Add(1 * time.Hour),
			})

			http.SetCookie(w, &http.Cookie{
				Name:     "pma_creds",
				Value:    sessionID,
				Path:     "/phpmyadmin/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
			})
			http.Redirect(w, req, "/phpmyadmin/?db="+parts[2], http.StatusFound)
			return
		}

		// Look up stored credentials from cookie
		var creds *pmaCredentials
		if cookie, err := req.Cookie("pma_creds"); err == nil {
			if val, ok := pmaStore.Load(cookie.Value); ok {
				c := val.(*pmaCredentials)
				if c.Expiry.After(time.Now()) {
					creds = c
				} else {
					pmaStore.Delete(cookie.Value)
				}
			}
		}

		if creds == nil {
			http.Redirect(w, req, "/", http.StatusFound)
			return
		}

		// Proxy to local Caddy phpMyAdmin backend (served via shared PHP-FPM pool)
		proxy := &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				r.URL.Scheme = "http"
				r.URL.Host = "127.0.0.1:2088"
				r.URL.Path = path
				if r.URL.Path == "" {
					r.URL.Path = "/"
				}
				r.Host = "127.0.0.1:2088"
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Del("WWW-Authenticate")
				return nil
			},
		}

		req.SetBasicAuth(creds.User, creds.Password)
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Header.Set("X-Forwarded-Proto", "https")
		proxy.ServeHTTP(w, req)
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
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
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
	workersCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
