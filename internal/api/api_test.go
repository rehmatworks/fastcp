package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Verbose() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	os.Exit(m.Run())
}

// fakeAgent implements the agent calls the panel makes in these tests.
type fakeAgent struct {
	mu        sync.Mutex
	state     agent.State
	applies   int
	failApply bool
	passwords map[string]string
	sites     map[string]bool
	dbs       map[string]bool
	logs      map[string]string // "user/slug/kind" -> contents
	caddy     agent.CaddyConfig
}

func (f *fakeAgent) handlers() map[string]agent.HandlerFunc {
	ok := map[string]string{"status": "ok"}
	decode := func(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }
	return map[string]agent.HandlerFunc{
		"ping": func(ctx context.Context, p json.RawMessage) (any, error) {
			return map[string]string{"version": "test"}, nil
		},
		"config.apply": func(ctx context.Context, p json.RawMessage) (any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.failApply {
				return nil, agent.NewError(agent.CodeInternal, "caddy validate failed")
			}
			var st agent.State
			if err := decode(p, &st); err != nil {
				return nil, err
			}
			f.state = st
			f.applies++
			return agent.ApplyResult{}, nil
		},
		"auth.verify": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.AuthRequest
			_ = decode(p, &req)
			f.mu.Lock()
			defer f.mu.Unlock()
			if pw, found := f.passwords[req.Username]; !found || pw != req.Password {
				return nil, agent.NewError(agent.CodeDenied, "authentication failed")
			}
			return ok, nil
		},
		"auth.changePassword": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.AuthRequest
			_ = decode(p, &req)
			f.mu.Lock()
			f.passwords[req.Username] = req.Password
			f.mu.Unlock()
			return ok, nil
		},
		"user.create": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.CreateUserRequest
			_ = decode(p, &req)
			f.mu.Lock()
			defer f.mu.Unlock()
			if _, found := f.passwords[req.Username]; found {
				return nil, agent.NewError(agent.CodeConflict, "user exists")
			}
			f.passwords[req.Username] = req.Password
			return ok, nil
		},
		"user.delete": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.DeleteUserRequest
			_ = decode(p, &req)
			f.mu.Lock()
			delete(f.passwords, req.Username)
			f.mu.Unlock()
			return ok, nil
		},
		"site.create": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.CreateSiteRequest
			_ = decode(p, &req)
			f.mu.Lock()
			f.sites[req.Username+"/"+req.Slug] = true
			f.mu.Unlock()
			return ok, nil
		},
		"site.delete": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.DeleteSiteRequest
			_ = decode(p, &req)
			f.mu.Lock()
			delete(f.sites, req.Username+"/"+req.Slug)
			f.mu.Unlock()
			return ok, nil
		},
		"database.create": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.CreateDatabaseRequest
			_ = decode(p, &req)
			f.mu.Lock()
			f.dbs[req.DBName] = true
			f.mu.Unlock()
			return ok, nil
		},
		"database.delete": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.DatabaseTarget
			_ = decode(p, &req)
			f.mu.Lock()
			delete(f.dbs, req.DBName)
			f.mu.Unlock()
			return ok, nil
		},
		"php.status": func(ctx context.Context, p json.RawMessage) (any, error) {
			return agent.PHPStatus{
				Versions: []agent.PHPVersionInfo{{Version: "8.3", Installed: true}, {Version: "8.4", Installed: true, Default: true}},
				Settings: agent.PHPSettings{},
			}, nil
		},
		"site.readLog": func(ctx context.Context, p json.RawMessage) (any, error) {
			var req agent.SiteLogRequest
			_ = decode(p, &req)
			f.mu.Lock()
			defer f.mu.Unlock()
			data, found := f.logs[req.Username+"/"+req.Slug+"/"+req.Kind]
			if !found {
				return agent.SiteLogResult{}, nil
			}
			return agent.SiteLogResult{Exists: true, Size: int64(len(data)), Data: data}, nil
		},
		"caddy.getConfig": func(ctx context.Context, p json.RawMessage) (any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.caddy, nil
		},
		"ssh.getConfig": func(ctx context.Context, p json.RawMessage) (any, error) { return agent.SSHConfig{Port: 2222}, nil },
		"system.status": func(ctx context.Context, p json.RawMessage) (any, error) {
			return agent.SystemStatus{Hostname: "test"}, nil
		},
		"system.services": func(ctx context.Context, p json.RawMessage) (any, error) { return []agent.ServiceStatus{}, nil },
		"rclone.status":   func(ctx context.Context, p json.RawMessage) (any, error) { return agent.ToolStatus{}, nil },
	}
}

func (f *fakeAgent) siteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.state.Sites)
}

type harness struct {
	t     *testing.T
	ts    *httptest.Server
	agent *fakeAgent
	db    *database.DB
	srv   *Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "fcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	fa := &fakeAgent{passwords: map[string]string{"fastcp": "admin-pass", "root": "root-pass"}, sites: map[string]bool{}, dbs: map[string]bool{}, logs: map[string]string{}}
	rpc := agent.NewRPCServer()
	for m, h := range fa.handlers() {
		rpc.Handle(m, h)
	}
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go rpc.Serve(ctx, ln)

	db, err := database.Open(filepath.Join(dir, "fastcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	adminFile := filepath.Join(dir, "default_admin")
	os.WriteFile(adminFile, []byte("fastcp\n"), 0o600)
	srv := New(Config{
		DB: db, Agent: agent.NewClient(sock), Box: crypto.New(bytes.Repeat([]byte{7}, 32)), Version: "3.0.0",
		UI:               fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}, "assets/app-1.js": {Data: []byte("js")}},
		DefaultAdminFile: adminFile,
	})
	srv.Start(ctx)
	ts := httptest.NewTLSServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		cancel()
		srv.Wait()
		db.Close()
	})
	return &harness{t: t, ts: ts, agent: fa, db: db, srv: srv}
}

type resp struct {
	status int
	body   map[string]any
	list   []any
	header http.Header
}

func (h *harness) do(method, path, token string, body any) resp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := h.ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header}
	if json.Unmarshal(raw, &out.body) != nil {
		_ = json.Unmarshal(raw, &out.list)
	}
	return out
}

func (h *harness) login(user, pass string) string {
	h.t.Helper()
	r := h.do("POST", "/api/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != 200 {
		h.t.Fatalf("login %s: %d %v", user, r.status, r.body)
	}
	return r.body["token"].(string)
}

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: status %d, want %d (%v)", what, r.status, status, r.body)
	}
}

func TestLoginAndSessions(t *testing.T) {
	h := newHarness(t)
	expect(t, h.do("POST", "/api/auth/login", "", map[string]string{"username": "root", "password": "root-pass"}), 403, "root login")
	expect(t, h.do("POST", "/api/auth/login", "", map[string]string{"username": "nobody1", "password": "x"}), 401, "unregistered login")
	expect(t, h.do("POST", "/api/auth/login", "", map[string]string{"username": "fastcp", "password": "wrong"}), 401, "bad password")

	token := h.login("fastcp", "admin-pass")
	me := h.do("GET", "/api/auth/me", token, nil)
	expect(t, me, 200, "me")
	if me.body["user"].(map[string]any)["is_admin"] != true {
		t.Fatalf("default admin not registered as admin: %v", me.body)
	}
	expect(t, h.do("GET", "/api/auth/me", "", nil), 401, "no token")
	expect(t, h.do("GET", "/api/auth/me", "garbage", nil), 401, "bad token")

	var stored string
	h.db.QueryRow("SELECT token_hash FROM auth_sessions LIMIT 1").Scan(&stored)
	if stored == token || stored != hashToken(token) {
		t.Fatal("session token is not stored as a hash")
	}

	expect(t, h.do("POST", "/api/auth/logout", token, nil), 204, "logout")
	expect(t, h.do("GET", "/api/auth/me", token, nil), 401, "after logout")
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t)
	h.login("fastcp", "admin-pass")
	var last resp
	for range 9 {
		last = h.do("POST", "/api/auth/login", "", map[string]string{"username": "fastcp", "password": "nope"})
	}
	expect(t, last, 429, "after repeated failures")
	expect(t, h.do("POST", "/api/auth/login", "", map[string]string{"username": "fastcp", "password": "admin-pass"}), 429, "correct password while locked")
}

func TestSitesUsersAndImpersonation(t *testing.T) {
	h := newHarness(t)
	admin := h.login("fastcp", "admin-pass")

	r := h.do("POST", "/api/users", admin, map[string]any{"username": "alice", "password": "alice-password", "max_sites": 1})
	expect(t, r, 201, "create user")
	expect(t, h.do("POST", "/api/users", admin, map[string]any{"username": "root", "password": "whatever123"}), 400, "reserved user")

	r = h.do("POST", "/api/sites", admin, map[string]any{"domain": "Example.COM."})
	expect(t, r, 201, "create site")
	siteID := r.body["id"].(string)
	if r.body["domain"] != "example.com" || r.body["slug"] != "example_com" || r.body["php_version"] != "8.4" {
		t.Fatalf("site = %v", r.body)
	}
	if h.agent.siteCount() != 1 {
		t.Fatal("site not pushed to the agent")
	}
	expect(t, h.do("POST", "/api/sites", admin, map[string]any{"domain": "example.com"}), 409, "duplicate domain")
	expect(t, h.do("POST", "/api/sites", admin, map[string]any{"domain": "10.0.0.1"}), 400, "IP domain")
	expect(t, h.do("POST", "/api/sites", admin, map[string]any{"domain": "x.example.org", "php_version": "7.4"}), 400, "missing PHP")

	r = h.do("POST", "/api/sites/"+siteID+"/domains", admin, map[string]any{"domain": "www.example.com"})
	expect(t, r, 201, "add domain")
	var aliasID float64
	for _, d := range r.body["domains"].([]any) {
		if d.(map[string]any)["domain"] == "www.example.com" {
			aliasID = d.(map[string]any)["id"].(float64)
		}
	}
	r = h.do("POST", "/api/sites/"+siteID+"/domains/"+jsonNum(aliasID)+"/primary", admin, nil)
	expect(t, r, 200, "set primary")
	if r.body["domain"] != "www.example.com" {
		t.Fatalf("primary not synced: %v", r.body["domain"])
	}
	expect(t, h.do("PATCH", "/api/sites/"+siteID, admin, map[string]any{"cache_control_enabled": true, "cache_control_value": "{file./etc/shadow}"}), 400, "cache-control injection")
	expect(t, h.do("PATCH", "/api/sites/"+siteID, admin, map[string]any{"php_post_max_size": "8M", "php_upload_max_filesize": "64M"}), 400, "post < upload")

	// Impersonation.
	expect(t, h.do("POST", "/api/auth/impersonate", admin, map[string]any{"username": "alice", "password": "wrong"}), 400, "impersonate with wrong password")
	r = h.do("POST", "/api/auth/impersonate", admin, map[string]any{"username": "alice", "password": "admin-pass", "reason": "support"})
	expect(t, r, 200, "impersonate")
	if r.body["user"].(map[string]any)["username"] != "alice" || r.body["actor"].(map[string]any)["username"] != "fastcp" {
		t.Fatalf("me while impersonating = %v", r.body)
	}
	expect(t, h.do("GET", "/api/users", admin, nil), 403, "admin route while impersonating")
	r = h.do("GET", "/api/sites", admin, nil)
	if r.status != 200 || len(r.list) != 0 {
		t.Fatalf("alice sees admin's sites: %d %v", r.status, r.list)
	}
	expect(t, h.do("POST", "/api/sites", admin, map[string]any{"domain": "alice.example.net"}), 201, "site as alice")
	expect(t, h.do("POST", "/api/sites", admin, map[string]any{"domain": "alice2.example.net"}), 403, "site limit")
	expect(t, h.do("GET", "/api/sites/"+siteID, admin, nil), 404, "other user's site")
	expect(t, h.do("DELETE", "/api/auth/impersonate", admin, nil), 200, "stop impersonating")
	expect(t, h.do("GET", "/api/users", admin, nil), 200, "admin route after impersonation")

	// Suspension revokes sessions.
	alice := h.login("alice", "alice-password")
	expect(t, h.do("GET", "/api/users", alice, nil), 403, "tenant on admin route")
	expect(t, h.do("POST", "/api/users/alice/suspend", admin, map[string]any{"suspended": true}), 200, "suspend")
	expect(t, h.do("GET", "/api/auth/me", alice, nil), 401, "suspended session")
	expect(t, h.do("POST", "/api/auth/login", "", map[string]string{"username": "alice", "password": "alice-password"}), 403, "suspended login")
	h.agent.mu.Lock()
	suspended := false
	for _, u := range h.agent.state.Users {
		if u.Username == "alice" {
			suspended = u.Suspended
		}
	}
	h.agent.mu.Unlock()
	if !suspended {
		t.Fatal("suspension not pushed to the agent")
	}

	expect(t, h.do("DELETE", "/api/users/fastcp", admin, nil), 400, "delete self")
	expect(t, h.do("DELETE", "/api/users/alice", admin, nil), 204, "delete alice")
	if h.agent.siteCount() != 1 {
		t.Fatalf("alice's site still configured (%d sites)", h.agent.siteCount())
	}
}

func jsonNum(f float64) string {
	b, _ := json.Marshal(int64(f))
	return string(b)
}

func TestFailedApplyIsRolledBack(t *testing.T) {
	h := newHarness(t)
	admin := h.login("fastcp", "admin-pass")
	h.agent.mu.Lock()
	h.agent.failApply = true
	h.agent.mu.Unlock()
	r := h.do("POST", "/api/cron", admin, map[string]any{"name": "job", "expression": "*/5 * * * *", "command": "php artisan schedule:run"})
	if r.status < 500 {
		t.Fatalf("cron create with a failing agent: %d", r.status)
	}
	jobs, _ := h.db.ListCronJobs(context.Background(), "fastcp")
	if len(jobs) != 0 {
		t.Fatal("cron job kept after the agent rejected the change")
	}
	expect(t, h.do("POST", "/api/cron", admin, map[string]any{"name": "bad", "expression": "* * * * *", "command": "echo hi\nrm -rf /"}), 400, "newline in command")
}

func TestHeadersAndSPA(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/health", "", nil)
	expect(t, r, 200, "health")
	if r.body["agent"] != "ok" {
		t.Fatalf("health = %v", r.body)
	}
	for _, p := range []string{"/", "/sites/abc"} {
		res, err := h.ts.Client().Get(h.ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), "app") {
			t.Fatalf("%s: %d %q", p, res.StatusCode, body)
		}
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: missing security headers: %v", p, res.Header)
		}
	}
	res, _ := h.ts.Client().Get(h.ts.URL + "/assets/missing.js")
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("missing asset: %d", res.StatusCode)
	}
	res, _ = h.ts.Client().Get(h.ts.URL + "/assets/app-1.js")
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset cache header = %q", res.Header.Get("Cache-Control"))
	}
}

func TestPhpMyAdminLinksAreSingleUse(t *testing.T) {
	h := newHarness(t)
	admin := h.login("fastcp", "admin-pass")
	r := h.do("POST", "/api/databases", admin, map[string]any{"name": "shop"})
	expect(t, r, 201, "create database")
	if r.body["db_name"] != "fastcp_shop" || r.body["password"] == "" {
		t.Fatalf("database = %v", r.body)
	}
	expect(t, h.do("POST", "/api/databases", admin, map[string]any{"name": "shop"}), 409, "duplicate database")
	id := r.body["id"].(string)

	link := h.do("POST", "/api/databases/"+id+"/phpmyadmin", admin, nil)
	expect(t, link, 200, "pma link")
	url := link.body["url"].(string)

	jar, _ := cookiejar.New(nil)
	client := h.ts.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Get(h.ts.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound || !strings.Contains(res.Header.Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("first use: %d %v", res.StatusCode, res.Header)
	}
	res, _ = h.ts.Client().Get(h.ts.URL + url)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("second use: %d", res.StatusCode)
	}
}

func TestClassifyAgentErrors(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
	}{
		{&agent.Error{Code: "invalid", Message: "x"}, 400},
		{&agent.Error{Code: "conflict", Message: "x"}, 409},
		{&agent.Error{Code: "denied", Message: "x"}, 403},
		{&agent.Error{Code: "internal", Message: "x"}, 502},
		{errors.Join(agent.ErrUnavailable), 503},
		{database.ErrNotFound, 404},
		{errors.New("boom"), 500},
	} {
		if got, _ := classify(c.err); got != c.status {
			t.Errorf("classify(%v) = %d, want %d", c.err, got, c.status)
		}
	}
}

func TestDescribeCron(t *testing.T) {
	for expr, want := range map[string]string{
		"* * * * *":    "Every minute",
		"*/15 * * * *": "Every 15 minutes",
		"30 * * * *":   "Every hour at minute 30",
		"0 2 * * *":    "Every day at 02:00",
		"0 */6 * * *":  "Every 6 hours at minute 0",
		"15 9 * * 1-5": "Weekdays at 09:15",
		"0 0 * * 0":    "Every Sunday at 00:00",
		"0 3 1 * *":    "On day 1 of every month at 03:00",
		"@daily":       "Every day at 00:00",
		"0 0 1 1 *":    "0 0 1 1 *",
	} {
		if got := describeCron(expr); got != want {
			t.Errorf("describeCron(%q) = %q, want %q", expr, got, want)
		}
	}
}
