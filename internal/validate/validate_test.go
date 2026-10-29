package validate

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNewUsername(t *testing.T) {
	good := []string{"alice", "a1", "web-01", "dev_team"}
	bad := []string{"", "a", "Alice", "1abc", "_x", "root", "caddy", "fastcp-panel", "systemd-foo", "a b", "abc\n", strings.Repeat("a", 33)}
	for _, s := range good {
		if err := NewUsername(s); err != nil {
			t.Errorf("NewUsername(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range bad {
		if err := NewUsername(s); err == nil {
			t.Errorf("NewUsername(%q) = nil, want error", s)
		}
	}
}

func TestTenantUsername(t *testing.T) {
	if err := TenantUsername("fastcp"); err != nil {
		t.Fatalf("fastcp must be a valid tenant: %v", err)
	}
	for _, s := range []string{"root", "www-data", "../etc", "a/b", ""} {
		if err := TenantUsername(s); err == nil {
			t.Errorf("TenantUsername(%q) = nil, want error", s)
		}
	}
}

func TestDomain(t *testing.T) {
	cases := map[string]string{
		"Example.COM":       "example.com",
		"example.com.":      "example.com",
		"sub.example.co.uk": "sub.example.co.uk",
		"xn--bcher-kva.de":  "xn--bcher-kva.de",
	}
	for in, want := range cases {
		got, err := Domain(in)
		if err != nil || got != want {
			t.Errorf("Domain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", "localhost", "foo.localhost", "1.2.3.4", "::1", "example", "-a.com", "a-.com",
		"exa mple.com", "example.123", "a..com", "{file./etc/passwd}.com", "a.com\nb.com", strings.Repeat("a", 64) + ".com"}
	for _, in := range bad {
		if _, err := Domain(in); err == nil {
			t.Errorf("Domain(%q) = nil error, want error", in)
		}
	}
}

func TestSlug(t *testing.T) {
	for _, s := range []string{"example-com", "a", "site_1"} {
		if err := Slug(s); err != nil {
			t.Errorf("Slug(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", ".", "..", "a/b", "a.b", "-a", "a-", strings.Repeat("a", 65)} {
		if err := Slug(s); err == nil {
			t.Errorf("Slug(%q) = nil, want error", s)
		}
	}
}

func TestDBIdentifiers(t *testing.T) {
	for _, s := range []string{"alice_wp", "john-doe_blog"} {
		if err := DBName(s); err != nil {
			t.Errorf("DBName(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "1db", "-ab", "a`b", "a'b", "a.b", "a b", strings.Repeat("a", 65)} {
		if err := DBName(s); err == nil {
			t.Errorf("DBName(%q) = nil, want error", s)
		}
	}
	if err := DBUser(strings.Repeat("a", 33)); err == nil {
		t.Error("DBUser accepted a 33 character name")
	}
}

func TestPHPSize(t *testing.T) {
	got, err := PHPSize(" 256m ", 1<<20, 4<<30)
	if err != nil || got != "256M" {
		t.Fatalf("PHPSize = %q, %v", got, err)
	}
	for _, s := range []string{"", "abc", "1T", "-1M", "256 M", "999999G"} {
		if _, err := PHPSize(s, 1<<20, 4<<30); err == nil {
			t.Errorf("PHPSize(%q) = nil error", s)
		}
	}
}

func TestCacheControl(t *testing.T) {
	got, err := CacheControl("Public, max-age=3600 , immutable")
	if err != nil || got != "public, max-age=3600, immutable" {
		t.Fatalf("CacheControl = %q, %v", got, err)
	}
	bad := []string{"", "{file./etc/shadow}", "max-age={env.SECRET}", "public\nX-Evil: 1", `public"`,
		"max-age", "public=1", "max-age=-1", "foo", "public, public"}
	for _, s := range bad {
		if _, err := CacheControl(s); err == nil {
			t.Errorf("CacheControl(%q) = nil error", s)
		}
	}
}

func TestSemVer(t *testing.T) {
	order := []string{"v2.0.4", "v3.0.0-rc.1", "v3.0.0-rc.2", "v3.0.0", "3.0.1", "v3.10.0"}
	for i := 0; i < len(order)-1; i++ {
		a, err := ParseSemVer(order[i])
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseSemVer(order[i+1])
		if err != nil {
			t.Fatal(err)
		}
		if a.Compare(b) != -1 || b.Compare(a) != 1 {
			t.Errorf("expected %s < %s", order[i], order[i+1])
		}
	}
	for _, s := range []string{"", "latest", "v3", "v3.0", "v3.0.0; rm -rf /", "../v3.0.0", "v03.0.0"} {
		if _, err := ParseSemVer(s); err == nil {
			t.Errorf("ParseSemVer(%q) = nil error", s)
		}
	}
	v, _ := ParseSemVer("3.0.0")
	if v.Tag() != "v3.0.0" {
		t.Errorf("Tag() = %q", v.Tag())
	}
}

func TestCron(t *testing.T) {
	for _, s := range []string{"*/5 * * * *", "0 2 * * 1-5", "@daily", "0 0 1 jan *"} {
		if err := CronExpression(s); err != nil {
			t.Errorf("CronExpression(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "* * * *", "* * * * * *", "@every 1m", "@reboot", "* * * * *\n* * * * * id", "61 * * * *"} {
		if err := CronExpression(s); err == nil {
			t.Errorf("CronExpression(%q) = nil error", s)
		}
	}
	for _, s := range []string{"@reboot", " @reboot ", "@daily", "*/5 * * * *"} {
		if err := CronJobSchedule(s); err != nil {
			t.Errorf("CronJobSchedule(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"@reboot\n* * * * * id", "@reboot x", "@every 1m"} {
		if err := CronJobSchedule(s); err == nil {
			t.Errorf("CronJobSchedule(%q) = nil error", s)
		}
	}
	if err := CronCommand("php artisan schedule:run\n* * * * * curl evil"); err == nil {
		t.Error("CronCommand accepted a newline")
	}
	if got := EscapeCronPercent(`date +%Y-\%m`); got != `date +\%Y-\%m` {
		t.Errorf("EscapeCronPercent = %q", got)
	}
}

func TestPasswordAndLabel(t *testing.T) {
	if err := Password("short"); err == nil {
		t.Error("short password accepted")
	}
	if err := Password("longenough\nroot:x"); err == nil {
		t.Error("password with newline accepted")
	}
	if err := Password("correct horse battery"); err != nil {
		t.Error(err)
	}
	if err := Label("name", "laptop\n", 64); err == nil {
		t.Error("label with newline accepted")
	}
}

func TestParseSSHPublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))

	k, err := ParseSSHPublicKey(line + " me@laptop")
	if err != nil {
		t.Fatal(err)
	}
	if k.Line != line || !strings.HasPrefix(k.Fingerprint, "SHA256:") {
		t.Fatalf("unexpected key %+v", k)
	}

	bad := []string{
		"",
		"command=\"/bin/sh\" " + line,
		line + "\n" + line,
		"ssh-ed25519 AAAA",
		"not a key",
	}
	for _, s := range bad {
		if _, err := ParseSSHPublicKey(s); err == nil {
			t.Errorf("ParseSSHPublicKey(%q) = nil error", s)
		}
	}

	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	smallPub, _ := ssh.NewPublicKey(&small.PublicKey)
	if _, err := ParseSSHPublicKey(string(ssh.MarshalAuthorizedKey(smallPub))); err == nil {
		t.Error("1024-bit RSA key accepted")
	}
}
