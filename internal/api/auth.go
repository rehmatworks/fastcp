package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	sessionIdle      = 2 * time.Hour
	sessionLifetime  = 24 * time.Hour
	impersonationMax = 2 * time.Hour
	touchInterval    = time.Minute
)

// principal is the authenticated caller. User is the account whose
// resources are being managed; it differs from Actor while an
// administrator is impersonating someone.
type principal struct {
	Actor     *database.User
	User      *database.User
	Session   *database.Session
	TokenHash string
}

func (p *principal) impersonating() bool { return p.User.Username != p.Actor.Username }

type principalKey struct{}

func current(r *http.Request) *principal {
	p, _ := r.Context().Value(principalKey{}).(*principal)
	return p
}

func newToken() (token, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) string {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

var errSignedOut = errf(http.StatusUnauthorized, "Your session has ended. Sign in again.")

// resolveSession validates a session token and returns the caller.
func (s *Server) resolveSession(ctx context.Context, token string) (*principal, error) {
	if token == "" || len(token) > 128 {
		return nil, errSignedOut
	}
	hash := hashToken(token)
	sess, err := s.db.GetSession(ctx, hash)
	if errors.Is(err, database.ErrNotFound) {
		return nil, errSignedOut
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > sessionIdle {
		_ = s.db.DeleteSession(ctx, hash)
		return nil, errSignedOut
	}
	actor, err := s.db.GetUser(ctx, sess.Username)
	if errors.Is(err, database.ErrNotFound) || (err == nil && actor.IsSuspended) {
		_ = s.db.DeleteSession(ctx, hash)
		return nil, errSignedOut
	}
	if err != nil {
		return nil, err
	}
	p := &principal{Actor: actor, User: actor, Session: sess, TokenHash: hash}
	if sess.ActingAs != "" {
		target, err := s.db.GetUser(ctx, sess.ActingAs)
		if err == nil && actor.IsAdmin && now.Before(sess.ActingUntil) && !target.IsAdmin && !target.IsSuspended {
			p.User = target
		} else {
			_ = s.db.SetSessionActingAs(ctx, hash, "", "", time.Time{})
			sess.ActingAs = ""
		}
	}
	if now.Sub(sess.LastSeenAt) > touchInterval {
		if err := s.db.TouchSession(ctx, hash, now); err != nil {
			slog.Warn("could not refresh session", "error", err)
		}
	}
	return p, nil
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.resolveSession(r.Context(), bearerToken(r))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := current(r)
		switch {
		case !p.Actor.IsAdmin:
			s.fail(w, r, forbidden("Administrator access is required."))
		case p.impersonating():
			s.fail(w, r, forbidden("Stop impersonating %s to use administrator tools.", p.User.Username))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// ---------------------------------------------------------------------------
// Rate limiting

type rateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]*rateEntry
}

type rateEntry struct {
	count int
	start time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, entries: map[string]*rateEntry{}}
}

// blocked reports whether key has used up its failures, and for how long.
func (l *rateLimiter) blocked(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil || now.Sub(e.start) > l.window {
		return false, 0
	}
	if e.count >= l.max {
		return true, l.window - now.Sub(e.start)
	}
	return false, 0
}

func (l *rateLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil || now.Sub(e.start) > l.window {
		e = &rateEntry{start: now}
		l.entries[key] = e
	}
	e.count++
}

func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *rateLimiter) gc(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		if now.Sub(e.start) > l.window {
			delete(l.entries, k)
		}
	}
}

func tooManyAttempts(wait time.Duration) error {
	minutes := int(wait.Minutes()) + 1
	return errf(http.StatusTooManyRequests, "Too many failed attempts. Try again in %d minute(s).", minutes)
}

// checkPassword verifies a system password through the agent, counting
// failures against both the client address and the account.
func (s *Server) checkPassword(ctx context.Context, r *http.Request, username, password string) error {
	now := time.Now()
	ip := clientIP(r)
	if b, wait := s.ipLimiter.blocked(ip, now); b {
		return tooManyAttempts(wait)
	}
	if b, wait := s.userLimiter.blocked(username, now); b {
		return tooManyAttempts(wait)
	}
	err := s.agent.VerifyPassword(ctx, username, password)
	if err == nil {
		s.userLimiter.reset(username)
		return nil
	}
	if code := agent.ErrorCode(err); code == "denied" || code == "invalid" || code == "not_found" {
		s.ipLimiter.fail(ip, now)
		s.userLimiter.fail(username, now)
		return errf(http.StatusUnauthorized, "The username or password is incorrect.")
	}
	return err
}

// ---------------------------------------------------------------------------
// Handlers

type userView struct {
	Username    string `json:"username"`
	IsAdmin     bool   `json:"is_admin"`
	IsSuspended bool   `json:"is_suspended"`
	MemoryMB    int    `json:"memory_mb"`
	CPUPercent  int    `json:"cpu_percent"`
	MaxSites    int    `json:"max_sites"`
	StorageMB   int    `json:"storage_mb"`
}

func viewUser(u *database.User) userView {
	return userView{u.Username, u.IsAdmin, u.IsSuspended, u.MemoryMB, u.CPUPercent, u.MaxSites, u.StorageMB}
}

type meResponse struct {
	User          userView   `json:"user"`
	Actor         userView   `json:"actor"`
	Impersonating bool       `json:"impersonating"`
	ActingUntil   *time.Time `json:"acting_until,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at"`
}

func meFor(p *principal) meResponse {
	m := meResponse{User: viewUser(p.User), Actor: viewUser(p.Actor), Impersonating: p.impersonating(), ExpiresAt: p.Session.ExpiresAt.UTC()}
	if m.Impersonating {
		t := p.Session.ActingUntil.UTC()
		m.ActingUntil = &t
	}
	return m
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	username := strings.TrimSpace(req.Username)
	invalid := errf(http.StatusUnauthorized, "The username or password is incorrect.")
	if username == "root" {
		return forbidden("Sign in with the FastCP administrator account, not root.")
	}
	if validate.TenantUsername(username) != nil || req.Password == "" || len(req.Password) > 1024 {
		return invalid
	}
	ctx := r.Context()
	user, err := s.db.GetUser(ctx, username)
	switch {
	case errors.Is(err, database.ErrNotFound):
		if username != s.defaultAdmin() {
			// Only accounts created through FastCP may sign in. Still check
			// the limiter so unknown names cannot be probed freely.
			s.ipLimiter.fail(clientIP(r), time.Now())
			return invalid
		}
	case err != nil:
		return err
	}
	if err := s.checkPassword(ctx, r, username, req.Password); err != nil {
		return err
	}
	if user == nil {
		user = &database.User{Username: username, IsAdmin: true, MemoryMB: -1, CPUPercent: -1, MaxSites: -1, StorageMB: -1}
		if err := s.change(ctx, func() (func(), error) {
			if err := s.db.CreateUser(ctx, user); err != nil && !errors.Is(err, database.ErrConflict) {
				return nil, err
			}
			return func() {}, nil
		}); err != nil {
			slog.Warn("default administrator registered but the agent was not updated", "error", err)
		}
		if user, err = s.db.GetUser(ctx, username); err != nil {
			return err
		}
	}
	if user.IsSuspended {
		return forbidden("This account is suspended. Contact your administrator.")
	}

	token, hash := newToken()
	now := time.Now()
	sess := &database.Session{
		TokenHash: hash, Username: username, IPAddress: clientIP(r), UserAgent: truncate(r.UserAgent(), 256),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(sessionLifetime),
	}
	if err := s.db.CreateSession(ctx, sess); err != nil {
		return err
	}
	p := &principal{Actor: user, User: user, Session: sess, TokenHash: hash}
	return ok(w, map[string]any{"token": token, "me": meFor(p)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if err := s.db.DeleteSession(r.Context(), current(r).TokenHash); err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	return noContent(w)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	return ok(w, meFor(current(r)))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	if p.impersonating() {
		return forbidden("You cannot change a password while impersonating.")
	}
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validate.Password(req.New); err != nil {
		return validation(err)
	}
	if err := s.checkPassword(r.Context(), r, p.Actor.Username, req.Current); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return badRequest("The current password is incorrect.")
		}
		return err
	}
	if err := s.agent.ChangePassword(r.Context(), p.Actor.Username, req.New); err != nil {
		return err
	}
	return noContent(w)
}

func (s *Server) startImpersonation(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Reason   string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 500 {
		return badRequest("Keep the reason under 500 characters.")
	}
	ctx := r.Context()
	target, err := s.db.GetUser(ctx, req.Username)
	if errors.Is(err, database.ErrNotFound) {
		return notFound("User")
	}
	if err != nil {
		return err
	}
	audit := &database.AuditEntry{Admin: p.Actor.Username, Target: target.Username, Reason: reason,
		IPAddress: clientIP(r), UserAgent: truncate(r.UserAgent(), 256)}
	switch {
	case target.Username == p.Actor.Username:
		return badRequest("You cannot impersonate yourself.")
	case target.IsAdmin:
		return forbidden("Administrators cannot be impersonated.")
	case target.IsSuspended:
		return forbidden("Unsuspend %s before impersonating them.", target.Username)
	}
	if err := s.checkPassword(ctx, r, p.Actor.Username, req.Password); err != nil {
		audit.Action = "start_failed"
		_ = s.db.AddAudit(ctx, audit)
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return badRequest("Your password is incorrect.")
		}
		return err
	}
	until := time.Now().Add(impersonationMax)
	if until.After(p.Session.ExpiresAt) {
		until = p.Session.ExpiresAt
	}
	if err := s.db.SetSessionActingAs(ctx, p.TokenHash, target.Username, reason, until); err != nil {
		return err
	}
	audit.Action = "start"
	if err := s.db.AddAudit(ctx, audit); err != nil {
		slog.Warn("could not write impersonation audit entry", "error", err)
	}
	slog.Info("impersonation started", "admin", p.Actor.Username, "target", target.Username)
	p.User = target
	p.Session.ActingAs, p.Session.ActingReason, p.Session.ActingUntil = target.Username, reason, until
	return ok(w, meFor(p))
}

func (s *Server) stopImpersonation(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	if !p.impersonating() {
		return ok(w, meFor(p))
	}
	ctx := r.Context()
	if err := s.db.SetSessionActingAs(ctx, p.TokenHash, "", "", time.Time{}); err != nil {
		return err
	}
	_ = s.db.AddAudit(ctx, &database.AuditEntry{Admin: p.Actor.Username, Target: p.User.Username, Action: "stop",
		IPAddress: clientIP(r), UserAgent: truncate(r.UserAgent(), 256)})
	p.User = p.Actor
	p.Session.ActingAs = ""
	return ok(w, meFor(p))
}

// defaultAdmin is the administrator account the installer created.
func (s *Server) defaultAdmin() string {
	if s.cfg.DefaultAdminFile == "" {
		return ""
	}
	b, err := os.ReadFile(s.cfg.DefaultAdminFile)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(b))
	if validate.TenantUsername(name) != nil {
		return ""
	}
	return name
}

// bootstrapAdmin registers the installer's administrator on a fresh
// database so the agent treats the account as a tenant from the start.
func (s *Server) bootstrapAdmin(ctx context.Context) {
	name := s.defaultAdmin()
	if name == "" {
		return
	}
	if _, err := s.db.GetUser(ctx, name); !errors.Is(err, database.ErrNotFound) {
		return
	}
	users, err := s.db.ListUsers(ctx)
	if err != nil || len(users) > 0 {
		return
	}
	u := &database.User{Username: name, IsAdmin: true, MemoryMB: -1, CPUPercent: -1, MaxSites: -1, StorageMB: -1}
	if err := s.db.CreateUser(ctx, u); err != nil {
		slog.Warn("could not register the default administrator", "user", name, "error", err)
		return
	}
	slog.Info("registered the default administrator", "user", name)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
