package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		slog.Error("health check: database unavailable", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "database": "unavailable"})
		return nil
	}
	agentStatus := "ok"
	if _, err := s.agent.Ping(ctx); err != nil {
		agentStatus = "unavailable"
	}
	return ok(w, map[string]string{"status": "ok", "version": s.cfg.Version, "agent": agentStatus})
}

func (s *Server) versionInfo(w http.ResponseWriter, r *http.Request) error {
	return ok(w, map[string]string{"version": s.cfg.Version})
}

type metaResponse struct {
	Version    string   `json:"version"`
	Hostname   string   `json:"hostname"`
	SSHPort    int      `json:"ssh_port"`
	PHP        []string `json:"php_versions"`
	DefaultPHP string   `json:"default_php"`
}

// meta is what every signed-in page needs to render forms.
func (s *Server) meta(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m := metaResponse{Version: s.cfg.Version, SSHPort: 22, PHP: []string{}}
	m.Hostname, _ = os.Hostname()
	if installed, def, err := s.installedPHP(ctx); err == nil {
		m.PHP, m.DefaultPHP = installed, def
		if m.PHP == nil {
			m.PHP = []string{}
		}
	}
	if cfg, err := s.agent.GetSSHConfig(ctx); err == nil && cfg.Port > 0 {
		m.SSHPort = cfg.Port
	}
	return ok(w, m)
}

type accountSummary struct {
	Sites     int                   `json:"sites"`
	Databases int                   `json:"databases"`
	CronJobs  int                   `json:"cron_jobs"`
	SSHKeys   int                   `json:"ssh_keys"`
	Limits    userView              `json:"limits"`
	Backup    *backupSummary        `json:"backup"`
	Recent    []*database.BackupJob `json:"recent_jobs"`
}

type backupSummary struct {
	Configured bool       `json:"configured"`
	Enabled    bool       `json:"enabled"`
	LastStatus string     `json:"last_status"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
}

type serverSummary struct {
	Status   *agent.SystemStatus    `json:"status,omitempty"`
	Services []agent.ServiceStatus  `json:"services"`
	Users    int                    `json:"users"`
	Sites    int                    `json:"sites"`
	Database int                    `json:"databases"`
	Update   *updateInfo            `json:"update,omitempty"`
	PHP      []agent.PHPVersionInfo `json:"php"`
	Errors   []string               `json:"errors,omitempty"`
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	ctx := r.Context()
	username := p.User.Username
	acct := accountSummary{Limits: viewUser(p.User), Recent: []*database.BackupJob{}}
	var err error
	if acct.Sites, err = s.db.CountSites(ctx, username); err != nil {
		return err
	}
	if acct.Databases, err = s.db.CountDatabases(ctx, username); err != nil {
		return err
	}
	crons, err := s.db.ListCronJobs(ctx, username)
	if err != nil {
		return err
	}
	acct.CronJobs = len(crons)
	keys, err := s.db.ListSSHKeys(ctx, username)
	if err != nil {
		return err
	}
	acct.SSHKeys = len(keys)
	acct.Backup = &backupSummary{LastStatus: "idle"}
	if cfg, err := s.db.GetBackupConfig(ctx, username); err == nil {
		acct.Backup = &backupSummary{Configured: cfg.Repository != "", Enabled: cfg.Enabled, LastStatus: cfg.LastStatus,
			LastRunAt: cfg.LastRunAt, NextRunAt: cfg.NextRunAt}
	} else if !errors.Is(err, database.ErrNotFound) {
		return err
	}
	if jobs, err := s.db.ListBackupJobs(ctx, username, 5); err == nil && jobs != nil {
		acct.Recent = jobs
	}

	out := map[string]any{"account": acct}
	if p.Actor.IsAdmin && !p.impersonating() {
		out["server"] = s.serverSummary(ctx)
	}
	return ok(w, out)
}

func (s *Server) serverSummary(ctx context.Context) *serverSummary {
	sum := &serverSummary{Services: []agent.ServiceStatus{}, PHP: []agent.PHPVersionInfo{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	fail := func(what string, err error) {
		mu.Lock()
		sum.Errors = append(sum.Errors, what+": "+err.Error())
		mu.Unlock()
	}
	wg.Add(3)
	go func() {
		defer wg.Done()
		st, err := s.agent.SystemStatus(ctx)
		if err != nil {
			fail("system status", err)
			return
		}
		mu.Lock()
		sum.Status = st
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		svcs, err := s.agent.Services(ctx)
		if err != nil {
			fail("services", err)
			return
		}
		mu.Lock()
		sum.Services = svcs
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		st, err := s.phpStatus(ctx)
		if err != nil {
			fail("PHP", err)
			return
		}
		mu.Lock()
		sum.PHP = st.Versions
		mu.Unlock()
	}()
	if users, err := s.db.ListUsers(ctx); err == nil {
		sum.Users = len(users)
	}
	if sites, err := s.db.ListAllSites(ctx); err == nil {
		sum.Sites = len(sites)
	}
	sum.Database, _ = s.db.CountDatabases(ctx, "")
	if info, ok := s.updates.peek(); ok {
		sum.Update = info
	}
	wg.Wait()
	return sum
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) error {
	svcs, err := s.agent.Services(r.Context())
	if err != nil {
		return err
	}
	return ok(w, svcs)
}

func (s *Server) getAgentJob(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if validate.Label("job id", id, 80) != nil {
		return notFound("Job")
	}
	job, err := s.agent.GetJob(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, job)
}
