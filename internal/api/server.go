// Package api is the FastCP control panel's HTTP API. It runs as the
// unprivileged fastcp-panel user and asks fastcp-agent for anything that
// needs root.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

// Config wires the server to its dependencies.
type Config struct {
	DB      *database.DB
	Agent   *agent.Client
	Box     *crypto.Box
	Version string
	// UI is the built single-page app (index.html and assets/).
	UI fs.FS
	// DefaultAdminFile names the account the installer created; it is
	// registered as an administrator on first sign-in.
	DefaultAdminFile string
	// PMABackend is the loopback address Caddy serves phpMyAdmin on.
	PMABackend string
	// AgentBinary is run as a tenant to build backup exports.
	AgentBinary string
}

// Server serves the panel API, the UI and the phpMyAdmin proxy.
type Server struct {
	cfg   Config
	db    *database.DB
	agent *agent.Client
	box   *crypto.Box

	// syncMu serializes changes that must be pushed to the agent, so the
	// agent always receives states in commit order.
	syncMu sync.Mutex

	ipLimiter   *rateLimiter
	userLimiter *rateLimiter
	pma         *pmaStore
	downloads   *tokenStore[downloadGrant]
	ssl         *sslChecker
	backups     *backupRunner
	php         cached[*agent.PHPStatus]
	updates     cached[*updateInfo]

	ctx context.Context
	wg  sync.WaitGroup
}

// New creates a server. Call Start to run background work.
func New(cfg Config) *Server {
	if cfg.PMABackend == "" {
		cfg.PMABackend = "127.0.0.1:2088"
	}
	if cfg.AgentBinary == "" {
		cfg.AgentBinary = "/opt/fastcp/bin/fastcp-agent"
	}
	s := &Server{
		cfg:         cfg,
		db:          cfg.DB,
		agent:       cfg.Agent,
		box:         cfg.Box,
		ipLimiter:   newRateLimiter(20, 15*time.Minute),
		userLimiter: newRateLimiter(8, 15*time.Minute),
		pma:         newPMAStore(),
		downloads:   newTokenStore[downloadGrant](),
		ctx:         context.Background(),
	}
	s.ssl = newSSLChecker()
	s.backups = newBackupRunner(s)
	return s
}

// Start launches background workers. They stop when ctx is cancelled;
// call Wait afterwards.
func (s *Server) Start(ctx context.Context) {
	s.ctx = ctx
	s.bootstrapAdmin(ctx)
	if err := s.db.ResetInterruptedBackups(ctx); err != nil {
		slog.Warn("could not reset interrupted backups", "error", err)
	}
	s.goBackground(s.initialSync)
	s.goBackground(s.housekeeping)
	s.goBackground(s.sslLoop)
	s.goBackground(s.backups.schedulerLoop)
	s.goBackground(s.updateLoop)
}

// updateLoop keeps the cached release check warm so the overview can show
// an available update without anyone opening the settings page.
func (s *Server) updateLoop(ctx context.Context) {
	wait := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		s.checkUpdates(ctx, true)
		wait = 6 * time.Hour
	}
}

func (s *Server) goBackground(fn func(context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(s.ctx)
	}()
}

// Wait blocks until background workers and running jobs have stopped.
func (s *Server) Wait() { s.wg.Wait() }

func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		now := time.Now()
		if err := s.db.DeleteStaleSessions(ctx, now, sessionIdle); err != nil && ctx.Err() == nil {
			slog.Warn("session cleanup failed", "error", err)
		}
		s.ipLimiter.gc(now)
		s.userLimiter.gc(now)
		s.pma.gc(now)
		s.downloads.gc(now)
		s.backups.cleanExports(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Handler returns the HTTP handler for the whole panel.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(recoverer, logRequests, baseHeaders)

	r.Route("/api", func(r chi.Router) {
		r.Use(apiHeaders, limitBody(1<<20), detachWrites)
		r.NotFound(s.h(func(w http.ResponseWriter, r *http.Request) error { return notFound("endpoint") }))
		r.MethodNotAllowed(s.h(func(w http.ResponseWriter, r *http.Request) error {
			return errf(http.StatusMethodNotAllowed, "method not allowed")
		}))

		r.Get("/health", s.h(s.health))
		r.Get("/version", s.h(s.versionInfo))
		r.Post("/auth/login", s.h(s.login))
		r.Get("/backups/download", s.downloadExport)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)

			r.Get("/auth/me", s.h(s.me))
			r.Post("/auth/logout", s.h(s.logout))
			r.Post("/auth/password", s.h(s.changePassword))
			r.Delete("/auth/impersonate", s.h(s.stopImpersonation))
			r.Get("/meta", s.h(s.meta))
			r.Get("/overview", s.h(s.overview))

			r.Get("/sites", s.h(s.listSites))
			r.Post("/sites", s.h(s.createSite))
			r.Get("/sites/check-domain", s.h(s.checkDomain))
			r.Get("/sites/{id}", s.h(s.getSite))
			r.Patch("/sites/{id}", s.h(s.updateSite))
			r.Delete("/sites/{id}", s.h(s.deleteSite))
			r.Post("/sites/{id}/domains", s.h(s.addDomain))
			r.Patch("/sites/{id}/domains/{domainID}", s.h(s.updateDomain))
			r.Post("/sites/{id}/domains/{domainID}/primary", s.h(s.setPrimaryDomain))
			r.Delete("/sites/{id}/domains/{domainID}", s.h(s.deleteDomain))
			r.Post("/sites/{id}/ssl-check", s.h(s.recheckSSL))

			r.Get("/databases", s.h(s.listDatabases))
			r.Post("/databases", s.h(s.createDatabase))
			r.Delete("/databases/{id}", s.h(s.deleteDatabase))
			r.Post("/databases/{id}/reset-password", s.h(s.resetDatabasePassword))
			r.Post("/databases/{id}/phpmyadmin", s.h(s.phpMyAdminLink))

			r.Get("/cron", s.h(s.listCron))
			r.Post("/cron", s.h(s.createCron))
			r.Post("/cron/describe", s.h(s.describeCron))
			r.Put("/cron/{id}", s.h(s.updateCron))
			r.Delete("/cron/{id}", s.h(s.deleteCron))

			r.Get("/ssh-keys", s.h(s.listSSHKeys))
			r.Post("/ssh-keys", s.h(s.addSSHKey))
			r.Delete("/ssh-keys/{id}", s.h(s.deleteSSHKey))

			r.Get("/backups", s.h(s.getBackups))
			r.Put("/backups/config", s.h(s.saveBackupConfig))
			r.Post("/backups/config/test", s.h(s.testBackupConfig))
			r.Post("/backups/sftp-key", s.h(s.sftpKey))
			r.Post("/backups/run", s.h(s.runBackup))
			r.Get("/backups/jobs", s.h(s.listBackupJobs))
			r.Get("/backups/jobs/{id}", s.h(s.getBackupJob))
			r.Get("/backups/snapshots", s.h(s.listSnapshots))
			r.Delete("/backups/snapshots/{snapshotID}", s.h(s.deleteSnapshot))
			r.Post("/backups/snapshots/{snapshotID}/restore", s.h(s.restoreSnapshot))
			r.Post("/backups/snapshots/{snapshotID}/export", s.h(s.exportSnapshot))
			r.Post("/backups/jobs/{id}/download-link", s.h(s.exportDownloadLink))

			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)

				r.Post("/auth/impersonate", s.h(s.startImpersonation))

				r.Get("/users", s.h(s.listUsers))
				r.Post("/users", s.h(s.createUser))
				r.Patch("/users/{username}", s.h(s.updateUser))
				r.Post("/users/{username}/suspend", s.h(s.suspendUser))
				r.Post("/users/{username}/password", s.h(s.setUserPassword))
				r.Delete("/users/{username}", s.h(s.deleteUser))

				r.Get("/system/services", s.h(s.services))
				r.Get("/jobs/{id}", s.h(s.getAgentJob))

				r.Get("/settings/php", s.h(s.getPHPSettings))
				r.Put("/settings/php", s.h(s.savePHPSettings))
				r.Post("/settings/php/install", s.h(s.installPHP))
				r.Post("/settings/php/maintenance", s.h(s.runPHPMaintenance))
				r.Get("/settings/webserver", s.h(s.getWebServer))
				r.Put("/settings/webserver", s.h(s.saveWebServer))
				r.Get("/settings/mysql", s.h(s.getMySQL))
				r.Put("/settings/mysql", s.h(s.saveMySQL))
				r.Get("/settings/ssh", s.h(s.getSSH))
				r.Put("/settings/ssh", s.h(s.saveSSH))
				r.Get("/settings/firewall", s.h(s.getFirewall))
				r.Post("/settings/firewall/install", s.h(s.installFirewall))
				r.Put("/settings/firewall/enabled", s.h(s.setFirewallEnabled))
				r.Post("/settings/firewall/rules", s.h(s.addFirewallRule))
				r.Post("/settings/firewall/rules/delete", s.h(s.deleteFirewallRule))
				r.Get("/settings/updates", s.h(s.getUpdates))
				r.Post("/settings/updates/apply", s.h(s.applyUpdate))
				r.Get("/settings/rclone", s.h(s.getRclone))
				r.Post("/settings/rclone/install", s.h(s.installRclone))
			})
		})
	})

	r.Handle("/phpmyadmin", http.RedirectHandler("/phpmyadmin/", http.StatusMovedPermanently))
	r.Handle("/phpmyadmin/*", http.HandlerFunc(s.phpMyAdmin))
	r.Get("/*", s.spa)
	return r
}
