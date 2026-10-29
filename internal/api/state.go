package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/validate"
)

// buildState renders the database into the agent's desired state.
func (s *Server) buildState(ctx context.Context) (*agent.State, error) {
	users, err := s.db.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := s.db.ListAllSites(ctx)
	if err != nil {
		return nil, err
	}
	domains, err := s.db.DomainsBySite(ctx)
	if err != nil {
		return nil, err
	}
	crons, err := s.db.ListAllCronJobs(ctx)
	if err != nil {
		return nil, err
	}

	st := &agent.State{Users: []agent.StateUser{}, Sites: []agent.StateSite{}}
	index := map[string]int{}
	for _, u := range users {
		if validate.TenantUsername(u.Username) != nil {
			continue
		}
		index[u.Username] = len(st.Users)
		st.Users = append(st.Users, agent.StateUser{
			Username: u.Username, Suspended: u.IsSuspended, MemoryMB: u.MemoryMB, CPUPercent: u.CPUPercent,
			Cron: []agent.CronJob{},
		})
	}
	for _, c := range crons {
		i, ok := index[c.Username]
		if !ok {
			continue
		}
		st.Users[i].Cron = append(st.Users[i].Cron, agent.CronJob{
			ID: c.ID, Name: c.Name, Expression: c.Expression, Command: c.Command, Enabled: c.Enabled,
		})
	}
	for _, site := range sites {
		if _, ok := index[site.Username]; !ok {
			continue
		}
		ss := agent.StateSite{
			ID: site.ID, Username: site.Username, Slug: site.Slug, PHPVersion: site.PHPVersion,
			ForceHTTPS: site.ForceHTTPS, CompressionEnabled: site.CompressionEnabled,
			GzipEnabled: site.GzipEnabled, ZstdEnabled: site.ZstdEnabled,
			PHPMemoryLimit: site.PHPMemoryLimit, PHPPostMaxSize: site.PHPPostMaxSize,
			PHPUploadMaxSize: site.PHPUploadMaxSize, PHPMaxExecutionTime: site.PHPMaxExecutionTime,
			PHPMaxInputVars: site.PHPMaxInputVars,
		}
		if site.SiteType == "proxy" {
			ss.ProxyURL = site.ProxyURL
		}
		if site.CacheControlEnabled {
			ss.CacheControl = site.CacheControlValue
		}
		for _, d := range domains[site.ID] {
			ss.Domains = append(ss.Domains, agent.StateDomain{Domain: d.Domain, Primary: d.IsPrimary, RedirectToPrimary: d.RedirectToPrimary})
		}
		if len(ss.Domains) == 0 {
			ss.Domains = []agent.StateDomain{{Domain: site.Domain, Primary: true}}
		}
		st.Sites = append(st.Sites, ss)
	}
	return st, nil
}

// pushLocked sends the current state to the agent. syncMu must be held.
func (s *Server) pushLocked(ctx context.Context) error {
	st, err := s.buildState(ctx)
	if err != nil {
		return fmt.Errorf("building state: %w", err)
	}
	res, err := s.agent.ApplyConfig(ctx, st)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		slog.Warn("agent", "warning", w)
	}
	return nil
}

// sync pushes the current state.
func (s *Server) sync(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.pushLocked(ctx)
}

// change applies a database change and pushes the resulting state. If the
// agent rejects it, undo reverts the database and the previous state is
// pushed again, so the database never claims something the server does
// not have.
func (s *Server) change(ctx context.Context, fn func() (undo func(), err error)) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	undo, err := fn()
	if err != nil {
		return err
	}
	if err := s.pushLocked(ctx); err != nil {
		if undo != nil {
			undo()
			if rerr := s.pushLocked(ctx); rerr != nil && !errors.Is(rerr, agent.ErrUnavailable) {
				slog.Error("could not restore the previous configuration", "error", rerr)
			}
		}
		return err
	}
	return nil
}

// initialSync pushes state at startup, retrying until the agent answers.
func (s *Server) initialSync(ctx context.Context) {
	delay := time.Second
	for {
		err := s.sync(ctx)
		if err == nil {
			slog.Info("configuration pushed to the agent")
			return
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("could not push configuration to the agent; retrying", "error", err, "in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, time.Minute)
	}
}

// cached holds one value for a short time.
type cached[T any] struct {
	mu    sync.Mutex
	value T
	at    time.Time
	valid bool
}

func (c *cached[T]) get(ttl time.Duration, load func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.at) < ttl {
		return c.value, nil
	}
	v, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	c.value, c.at, c.valid = v, time.Now(), true
	return v, nil
}

// peek returns the last loaded value, however old.
func (c *cached[T]) peek() (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value, c.valid
}

func (c *cached[T]) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

func (s *Server) phpStatus(ctx context.Context) (*agent.PHPStatus, error) {
	return s.php.get(30*time.Second, func() (*agent.PHPStatus, error) { return s.agent.PHPStatus(ctx) })
}

// installedPHP returns installed PHP versions and the default version.
func (s *Server) installedPHP(ctx context.Context) ([]string, string, error) {
	st, err := s.phpStatus(ctx)
	if err != nil {
		return nil, "", err
	}
	var out []string
	def := ""
	for _, v := range st.Versions {
		if v.Installed {
			out = append(out, v.Version)
		}
		if v.Default {
			def = v.Version
		}
	}
	return out, def, nil
}
