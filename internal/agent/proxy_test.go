package agent

import (
	"strings"
	"testing"
)

func TestRenderProxySites(t *testing.T) {
	st := &State{
		Users: []StateUser{{Username: "alice"}, {Username: "bob"}},
		Sites: []StateSite{
			{
				ID: "11111111-0000-0000-0000-000000000000", Username: "alice", Slug: "app", ProxyURL: "127.0.0.1:3000", PHPVersion: "8.4",
				Domains:    []StateDomain{{Domain: "app.example.com", Primary: true}, {Domain: "www.app.example.com", RedirectToPrimary: true}},
				ForceHTTPS: true, CompressionEnabled: true, GzipEnabled: true,
			},
			{
				ID: "21111111-0000-0000-0000-000000000000", Username: "bob", Slug: "remote", ProxyURL: "https://backend.example.net",
				Domains: []StateDomain{{Domain: "remote.example.com", Primary: true}},
			},
			{
				ID: "31111111-0000-0000-0000-000000000000", Username: "alice", Slug: "panel", ProxyURL: "http://127.0.0.1:2050",
				Domains: []StateDomain{{Domain: "panel.example.com", Primary: true}},
			},
			{
				ID: "41111111-0000-0000-0000-000000000000", Username: "alice", Slug: "hostile", ProxyURL: "http://127.0.0.1:3000 {\n}\nimport /etc/passwd",
				Domains: []StateDomain{{Domain: "evil.example.com", Primary: true}},
			},
			{
				ID: "51111111-0000-0000-0000-000000000000", Username: "bob", Slug: "php-site", PHPVersion: "8.4",
				Domains: []StateDomain{{Domain: "panel.example.com", Primary: true}},
			},
		},
	}
	p := buildPlan(st, testLookup, map[string]bool{"8.4": true}, "8.4")
	if len(p.Sites) != 3 {
		t.Fatalf("want the two valid proxy sites and the PHP site, got %d: %v", len(p.Sites), p.Warnings)
	}
	for _, s := range p.Sites {
		if s.ProxyURL != "" && (s.PHPVersion != "" || s.Socket != "") {
			t.Errorf("proxy site %s has a PHP runtime: %q %q", s.Slug, s.PHPVersion, s.Socket)
		}
		if s.Slug == "php-site" && s.Primary != "panel.example.com" {
			t.Errorf("a skipped proxy site must not claim domains: %+v", s)
		}
	}

	out := renderCaddyfile(p, defaultCaddyConfig(1024), renderOptions{Admin: "unix/" + CaddyAdminSocket, OwnerFS: true})
	for _, want := range []string{
		"\nhttps://app.example.com {\n\tencode gzip\n\treverse_proxy http://127.0.0.1:3000\n}\n",
		"\nhttps://www.app.example.com {\n\tredir https://app.example.com{uri} permanent",
		"\nhttp://remote.example.com, https://remote.example.com {\n\treverse_proxy https://backend.example.net {\n\t\theader_up Host {upstream_hostport}\n\t}\n}\n",
		"filesystem fastcp_bob fastcp_owner 1002",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
	for _, bad := range []string{"127.0.0.1:2050", "/etc/passwd", "fastcp_alice", "/home/alice/apps/app/public"} {
		if strings.Contains(out, bad) {
			t.Errorf("Caddyfile contains %q\n%s", bad, out)
		}
	}
}

func TestRenderCustomImports(t *testing.T) {
	p := buildPlan(testState(), testLookup, map[string]bool{"8.3": true, "8.4": true}, "8.4")
	out := renderCaddyfile(p, defaultCaddyConfig(1024), renderOptions{Admin: "unix/" + CaddyAdminSocket, Custom: []customFile{
		{Rel: "extra.caddy", Hash: "0123456789abcdef"},
		{Rel: "sites/alice/example-com.caddy", Site: "alice/example-com", Hash: "fedcba9876543210"},
		{Rel: "sites/bob/bob-site.caddy", Site: "bob/bob-site", Hash: "1111111111111111"},
	}})
	global := "\nimport /opt/fastcp/config/caddy.applied/0123456789abcdef/extra.caddy\n"
	for _, want := range []string{
		"# Put your own configuration in /opt/fastcp/config/caddy.d instead",
		global,
		"\tfile_server\n\timport /opt/fastcp/config/caddy.applied/fedcba9876543210/sites/alice/example-com.caddy\n}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "1111111111111111") {
		t.Errorf("a suspended site must not load custom configuration:\n%s", out)
	}
	if strings.Index(out, global) > strings.Index(out, "# alice/") {
		t.Errorf("top-level files must come before the sites that may use their snippets:\n%s", out)
	}
}
