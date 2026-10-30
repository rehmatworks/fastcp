package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "fastcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testSite(id, user, domain, slug string) *Site {
	return &Site{
		ID: id, Username: user, Domain: domain, Slug: slug, SiteType: "php",
		DocumentRoot: "/home/" + user + "/apps/" + slug + "/public", PHPVersion: "8.4",
		PHPMemoryLimit: "256M", PHPPostMaxSize: "64M", PHPUploadMaxSize: "64M",
		PHPMaxExecutionTime: 300, PHPMaxInputVars: 5000,
	}
}

// TestMigrateFromV2 builds a FastCP 2 style database by hand and checks
// that opening it upgrades the schema without losing data.
func TestMigrateFromV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fastcp.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL, is_admin INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT UNIQUE NOT NULL, username TEXT NOT NULL,
			expires_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE sites (id TEXT PRIMARY KEY, username TEXT NOT NULL, domain TEXT UNIQUE NOT NULL, site_type TEXT NOT NULL DEFAULT 'php',
			document_root TEXT NOT NULL, ssl_enabled INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE site_domains (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id TEXT NOT NULL, domain TEXT UNIQUE NOT NULL,
			is_primary INTEGER DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO users (username, is_admin) VALUES ('alice', 0)`,
		`INSERT INTO sessions (token, username, expires_at) VALUES ('plaintext', 'alice', '2099-01-01 00:00:00')`,
		`INSERT INTO sites (id, username, domain, document_root) VALUES ('s1', 'alice', 'old.example.com', '/home/alice/apps/blog/public')`,
		`INSERT INTO site_domains (site_id, domain, is_primary) VALUES ('s1', 'new.example.com', 1)`,
	} {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("user_version = %d, %v", v, err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'sessions'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("plaintext sessions table still present (%d, %v)", n, err)
	}
	u, err := db.GetUser(context.Background(), "alice")
	if err != nil || u.MemoryMB != 512 || u.MaxSites != -1 || u.SiteCount != 1 {
		t.Fatalf("user = %+v, %v", u, err)
	}
	s, err := db.GetSite(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Slug != "blog" || s.Domain != "new.example.com" || !s.ForceHTTPSAuto {
		t.Fatalf("site after migration = %+v", s)
	}

	// Opening again is a no-op.
	db.Close()
	if db, err = Open(path); err != nil {
		t.Fatal(err)
	}
}

// TestLaterColumns opens a database written by an earlier FastCP 3 build:
// the new columns are added and the schema version stays, so that build
// can still open it after a rolled-back update.
func TestLaterColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fastcp.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range laterColumns {
		if _, err := db.Exec("ALTER TABLE " + c.table + " DROP COLUMN " + c.name); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	if db, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateUser(ctx, &User{Username: "alice", MemoryMB: 512, CPUPercent: 100, MaxSites: -1, StorageMB: -1}); err != nil {
		t.Fatal(err)
	}
	site := testSite("s1", "alice", "app.example.com", "app")
	site.SiteType, site.ProxyURL, site.PHPVersion = "proxy", "http://127.0.0.1:3000", ""
	if err := db.CreateSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	site.ProxyURL = "http://127.0.0.1:4000"
	if err := db.UpdateSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSite(ctx, "s1")
	if err != nil || got.SiteType != "proxy" || got.ProxyURL != "http://127.0.0.1:4000" || got.PageCache {
		t.Fatalf("site = %+v, %v", got, err)
	}
	wp := testSite("s2", "alice", "wp.example.com", "wp")
	wp.PageCache = true
	if err := db.CreateSite(ctx, wp); err != nil {
		t.Fatal(err)
	}
	wp.PageCachePhones = true
	if err := db.UpdateSite(ctx, wp); err != nil {
		t.Fatal(err)
	}
	if got, err = db.GetSite(ctx, "s2"); err != nil || !got.PageCache || !got.PageCachePhones {
		t.Fatalf("site = %+v, %v", got, err)
	}
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 3 {
		t.Fatalf("user_version = %d, %v", v, err)
	}
}

func TestSitesAndDomains(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	for _, u := range []string{"alice", "bob"} {
		if err := db.CreateUser(ctx, &User{Username: u, MemoryMB: 512, CPUPercent: 100, MaxSites: -1, StorageMB: -1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateSite(ctx, testSite("s1", "alice", "a.example.com", "a")); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSite(ctx, testSite("s2", "bob", "a.example.com", "a")); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate domain across users: %v", err)
	}
	if err := db.CreateSite(ctx, testSite("s3", "alice", "b.example.com", "a")); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug: %v", err)
	}

	alias := &SiteDomain{SiteID: "s1", Domain: "www.a.example.com"}
	if err := db.AddDomain(ctx, alias, 20); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSite(ctx, testSite("s4", "bob", "www.a.example.com", "w")); !errors.Is(err, ErrConflict) {
		t.Fatalf("site reused an alias: %v", err)
	}
	if err := db.AddDomain(ctx, &SiteDomain{SiteID: "s1", Domain: "c.example.com"}, 2); !errors.Is(err, ErrLimit) {
		t.Fatalf("domain limit: %v", err)
	}
	if err := db.SetPrimaryDomain(ctx, "s1", alias.ID); err != nil {
		t.Fatal(err)
	}
	s, _ := db.GetSite(ctx, "s1")
	if s.Domain != "www.a.example.com" {
		t.Fatalf("sites.domain = %q after SetPrimaryDomain", s.Domain)
	}
	domains, _ := db.ListDomains(ctx, "s1")
	if len(domains) != 2 || !domains[0].IsPrimary || domains[0].Domain != "www.a.example.com" {
		t.Fatalf("domains = %+v", domains)
	}
	if err := db.DeleteDomain(ctx, "s1", alias.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted the primary domain: %v", err)
	}
	if err := db.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSite(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("site survived its owner: %v", err)
	}
	if taken, _ := db.DomainTaken(ctx, "www.a.example.com"); taken {
		t.Fatal("domain still taken after the site was deleted")
	}
}

func TestSessions(t *testing.T) {
	// A non-UTC local zone must not affect expiry checks.
	orig := time.Local
	time.Local = time.FixedZone("UTC+5", 5*3600)
	t.Cleanup(func() { time.Local = orig })

	db := openTest(t)
	ctx := context.Background()
	for _, u := range []string{"admin", "alice"} {
		if err := db.CreateUser(ctx, &User{Username: u}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	s := &Session{TokenHash: "h1", Username: "admin", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionActingAs(ctx, "h1", "alice", "support", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSession(ctx, "h1")
	if err != nil || got.ActingAs != "alice" || !got.ExpiresAt.Equal(s.ExpiresAt.Truncate(time.Second)) {
		t.Fatalf("session = %+v, %v", got, err)
	}
	if err := db.RevokeUserSessions(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetSession(ctx, "h1"); got == nil || got.ActingAs != "" {
		t.Fatalf("impersonation survived revoking the target: %+v", got)
	}
	if err := db.DeleteStaleSessions(ctx, now.Add(2*time.Hour), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSession(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session kept: %v", err)
	}
}

func TestBackupClaim(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	if err := db.CreateUser(ctx, &User{Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(-time.Minute)
	cfg := &BackupConfig{Username: "alice", BackendType: "local", Repository: "/srv/b", PasswordEnc: "x",
		Enabled: true, ScheduleCron: "0 2 * * *", NextRunAt: &next, KeepLast: 7}
	if err := db.SaveBackupConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	due, err := db.DueBackups(ctx, time.Now())
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v, %v", due, err)
	}
	if _, err := db.ClaimBackupRun(ctx, "alice", &BackupJob{ID: "j1", JobType: "scheduled"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimBackupRun(ctx, "alice", &BackupJob{ID: "j2", JobType: "manual"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second claim: %v", err)
	}
	if err := db.ResetInterruptedBackups(ctx); err != nil {
		t.Fatal(err)
	}
	j, _ := db.GetBackupJob(ctx, "j1")
	if j.Status != "failed" || j.FinishedAt == nil {
		t.Fatalf("interrupted job = %+v", j)
	}
	got, _ := db.GetBackupConfig(ctx, "alice")
	if got.RunningJobID != "" || got.KeepLast != 7 || got.Repository != "/srv/b" {
		t.Fatalf("config after reset = %+v", got)
	}
}
