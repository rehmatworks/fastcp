package agent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testLookup(name string) (*tenant, error) {
	switch name {
	case "alice":
		return &tenant{Name: "alice", UID: 1001, GID: 1001, Home: "/home/alice"}, nil
	case "bob":
		return &tenant{Name: "bob", UID: 1002, GID: 1002, Home: "/home/bob"}, nil
	}
	return nil, errors.New("no such user")
}

func testState() *State {
	return &State{
		Users: []StateUser{{Username: "alice", MemoryMB: 1024}, {Username: "bob", Suspended: true}, {Username: "root"}},
		Sites: []StateSite{
			{
				ID: "11111111-2222-3333-4444-555555555555", Username: "alice", Slug: "example-com", PHPVersion: "8.3",
				Domains: []StateDomain{
					{Domain: "Example.com", Primary: true},
					{Domain: "www.example.com", RedirectToPrimary: true},
					{Domain: "alias.example.com"},
				},
				ForceHTTPS: true, CompressionEnabled: true, GzipEnabled: true, ZstdEnabled: true,
				CacheControl: "public, max-age=600",
			},
			{
				ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Username: "bob", Slug: "bob-site", PHPVersion: "8.4",
				Domains: []StateDomain{{Domain: "bob.test", Primary: true}},
			},
		},
	}
}

func TestBuildPlanValidatesAndDefaults(t *testing.T) {
	p := buildPlan(testState(), testLookup, map[string]bool{"8.4": true}, "8.4")
	if len(p.Sites) != 2 {
		t.Fatalf("expected 2 sites, got %d (%v)", len(p.Sites), p.Warnings)
	}
	if _, ok := p.Users["root"]; ok {
		t.Fatal("root must never be a tenant")
	}
	a := p.Sites[0]
	if a.PHPVersion != "8.4" {
		t.Fatalf("missing PHP 8.3 should fall back to 8.4, got %q", a.PHPVersion)
	}
	if a.Primary != "example.com" || len(a.Redirects) != 1 || len(a.Serving) != 2 {
		t.Fatalf("unexpected domain split: primary=%s serving=%v redirects=%v", a.Primary, a.Serving, a.Redirects)
	}
	if a.DocRoot != "/home/alice/apps/example-com/public" {
		t.Fatalf("doc root = %s", a.DocRoot)
	}
	if a.Socket != "/opt/fastcp/run/php/alice/111111112222-php84.sock" {
		t.Fatalf("socket = %s", a.Socket)
	}
	if !p.Sites[1].Suspended {
		t.Fatal("bob's site must be suspended")
	}
	if len(p.Warnings) == 0 {
		t.Fatal("expected a warning about PHP 8.3 and root")
	}
}

func TestBuildPlanRejectsHostileInput(t *testing.T) {
	st := &State{
		Users: []StateUser{{Username: "alice"}, {Username: "../etc"}},
		Sites: []StateSite{
			{ID: "11111111-0000-0000-0000-000000000001", Username: "alice", Slug: "../../etc", Domains: []StateDomain{{Domain: "a.com"}}},
			{ID: "11111111-0000-0000-0000-000000000002", Username: "alice", Slug: "ok",
				Domains:        []StateDomain{{Domain: "{file./etc/shadow}.com"}, {Domain: "b.com\nc.com"}, {Domain: "good.com"}},
				CacheControl:   `max-age=1 {file./etc/shadow}`,
				PHPMemoryLimit: "128M\nuser = root",
			},
			{ID: "11111111-0000-0000-0000-000000000003", Username: "alice", Slug: "dupe", Domains: []StateDomain{{Domain: "GOOD.com"}}},
			{ID: "11111111-0000-0000-0000-000000000004", Username: "mallory", Slug: "x", Domains: []StateDomain{{Domain: "m.com"}}},
		},
	}
	p := buildPlan(st, testLookup, map[string]bool{"8.4": true}, "8.4")
	if len(p.Sites) != 1 {
		t.Fatalf("expected exactly one valid site, got %d: %v", len(p.Sites), p.Warnings)
	}
	s := p.Sites[0]
	if s.Slug != "ok" || len(s.Serving) != 1 || s.Serving[0] != "good.com" {
		t.Fatalf("unexpected site %+v", s)
	}
	if s.CacheControl != "" || s.PHPMemoryLimit != "256M" {
		t.Fatalf("hostile values not reset: cc=%q mem=%q", s.CacheControl, s.PHPMemoryLimit)
	}
	out := renderCaddyfile(p, defaultCaddyConfig(1024), renderOptions{Admin: "unix/" + CaddyAdminSocket})
	for _, bad := range []string{"{file.", "/etc/shadow", "user = root", "mallory"} {
		if strings.Contains(out, bad) {
			t.Fatalf("Caddyfile contains %q:\n%s", bad, out)
		}
	}
}

func TestRenderCaddyfile(t *testing.T) {
	p := buildPlan(testState(), testLookup, map[string]bool{"8.3": true, "8.4": true}, "8.4")
	cfg := defaultCaddyConfig(1024)
	cfg.AccessLogs = true
	out := renderCaddyfile(p, cfg, renderOptions{Admin: "unix/" + CaddyAdminSocket, PMASocket: pmaSocketPath()})
	for _, want := range []string{
		"admin unix//run/fastcp-caddy/admin.sock",
		"storage file_system /var/lib/fastcp/caddy",
		"persist_config off",
		"bind 127.0.0.1",
		"\nhttps://example.com, https://alias.example.com {",
		`header Cache-Control "public, max-age=600"`,
		"encode zstd gzip",
		"php_fastcgi unix//opt/fastcp/run/php/alice/111111112222-php83.sock",
		"output file /var/log/fastcp/sites/alice/example-com/access.log",
		"\nhttps://www.example.com {\n\tredir https://example.com{uri} permanent",
		"\nhttp://example.com, http://alias.example.com, http://www.example.com {\n\t@cloudflare_flexible header CF-Visitor *https*\n\troute {\n\t\trespond @cloudflare_flexible ",
		"\t\tredir https://{host}{uri} 308\n\t}\n}",
		"root * /opt/fastcp/www/suspended",
		"\nhttp://bob.test, https://bob.test {",
		"import fastcp_hidden",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "localhost:2019") {
		t.Error("admin API must not listen on TCP")
	}
	if n := strings.Count(out, "@cloudflare_flexible header"); n != 1 {
		t.Errorf("want an HTTP redirect block only for the site forcing HTTPS, got %d", n)
	}
	if again := renderCaddyfile(p, cfg, renderOptions{Admin: "unix/" + CaddyAdminSocket, PMASocket: pmaSocketPath()}); again != out {
		t.Error("render is not deterministic")
	}

	dev := renderCaddyfile(p, cfg, renderOptions{Dev: true, Admin: "unix/" + CaddyAdminSocket})
	if !strings.Contains(dev, "auto_https off") || !strings.Contains(dev, "http://example.com, http://alias.example.com {") {
		t.Errorf("dev render unexpected:\n%s", dev)
	}
	if strings.Contains(dev, "https://") || strings.Contains(dev, "cloudflare_flexible") {
		t.Errorf("dev render must be plain HTTP:\n%s", dev)
	}
}

func TestRenderSuspendedForcedHTTPS(t *testing.T) {
	st := testState()
	for i := range st.Users {
		st.Users[i].Suspended = true
	}
	out := renderCaddyfile(buildPlan(st, testLookup, map[string]bool{"8.3": true, "8.4": true}, "8.4"), defaultCaddyConfig(1024), renderOptions{Admin: "unix/" + CaddyAdminSocket})
	for _, want := range []string{
		"\nhttps://example.com, https://alias.example.com, https://www.example.com {\n\troot * /opt/fastcp/www/suspended",
		"\nhttp://example.com, http://alias.example.com, http://www.example.com {\n\t@cloudflare_flexible",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
}

func TestRenderOwnerFS(t *testing.T) {
	p := buildPlan(testState(), testLookup, map[string]bool{"8.3": true, "8.4": true}, "8.4")
	opts := renderOptions{Admin: "unix/" + CaddyAdminSocket, PMASocket: pmaSocketPath()}
	if out := renderCaddyfile(p, defaultCaddyConfig(1024), opts); strings.Contains(out, "filesystem") || strings.Contains(out, "\tfs ") {
		t.Errorf("file systems rendered for a Caddy without them:\n%s", out)
	}
	opts.OwnerFS = true
	out := renderCaddyfile(p, defaultCaddyConfig(1024), opts)
	for _, want := range []string{
		"\tstorage file_system /var/lib/fastcp/caddy\n\tfilesystem fastcp_alice fastcp_owner 1001\n",
		"\troot * /home/alice/apps/example-com/public\n\tfs fastcp_alice\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
	// bob is suspended; his sites, and phpMyAdmin, serve FastCP's own files.
	if strings.Contains(out, "fastcp_bob") || strings.Count(out, "\tfs ") != 1 {
		t.Errorf("unexpected file systems:\n%s", out)
	}
}

func TestRenderPageCache(t *testing.T) {
	st := testState()
	opts := renderOptions{Admin: "unix/" + CaddyAdminSocket}
	installed := map[string]bool{"8.4": true}
	if out := renderCaddyfile(buildPlan(st, testLookup, installed, "8.4"), defaultCaddyConfig(1024), opts); strings.Contains(out, "fastcp_page") {
		t.Fatalf("page cache rendered while off:\n%s", out)
	}

	st.Sites[0].PageCache = true
	out := renderCaddyfile(buildPlan(st, testLookup, installed, "8.4"), defaultCaddyConfig(1024), opts)
	for _, want := range []string{
		"\tmap {header.User-Agent} {fastcp_page_device} {\n\t\t\"~(Mobile|Android|",
		"\t\tmethod GET HEAD\n\t\tprotocol https\n\t\tpath */\n",
		"\t\tnot header_regexp Cookie \"(wordpress_logged_in|wp-postpass_|",
		"\t\texpression `{query} == \"\"`\n",
		"\t\tfile /wp-content/cache/wp-rocket/{host}{path}index{fastcp_page_device}-https.html " +
			"/wp-content/cache/supercache/{host}{path}index{fastcp_page_device}-https.html\n",
		"\troute @fastcp_page_cache {\n\t\trewrite {file_match.relative}\n\t\theader X-FastCP-Cache HIT\n\t\theader ?Cache-Control no-cache\n\t}\n\tphp_fastcgi ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Caddyfile missing %q\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\t@fastcp_page_cache {"); n != 1 {
		t.Errorf("page cache rendered %d times, want only for the enabled site", n)
	}

	st.Sites[0].PageCachePhones = true
	out = renderCaddyfile(buildPlan(st, testLookup, installed, "8.4"), defaultCaddyConfig(1024), opts)
	if want := "\t\tfile /wp-content/cache/wp-rocket/{host}{path}index{fastcp_page_device}-https.html " +
		"/wp-content/cache/wp-rocket/{host}{path}index-https.html /wp-content/cache/supercache/{host}{path}index-https.html\n"; !strings.Contains(out, want) {
		t.Errorf("Caddyfile missing %q\n%s", want, out)
	}

	opts.Dev = true
	if dev := renderCaddyfile(buildPlan(st, testLookup, installed, "8.4"), defaultCaddyConfig(1024), opts); strings.Contains(dev, "fastcp_page") {
		t.Errorf("page cache rendered for plain HTTP:\n%s", dev)
	}
}

func TestReadTail(t *testing.T) {
	for _, tc := range []struct {
		data, want string
		limit      int64
		truncated  bool
	}{
		{"one\ntwo\n", "one\ntwo\n", 100, false},
		{"one\ntwo\nthree\n", "three\n", 8, true},
		{"one\ntwo\nthree\n", "two\nthree\n", 11, true},
		{"one\ntwo\nthree\n", "two\nthree\n", 10, true},
		{"averyverylongline", "", 4, true},
		{"", "", 10, false},
	} {
		got, truncated, err := readTail(strings.NewReader(tc.data), int64(len(tc.data)), tc.limit)
		if err != nil || got != tc.want || truncated != tc.truncated {
			t.Errorf("readTail(%q, %d) = %q, %v, %v; want %q, %v", tc.data, tc.limit, got, truncated, err, tc.want, tc.truncated)
		}
	}
}

func TestRenderSitePool(t *testing.T) {
	p := buildPlan(testState(), testLookup, map[string]bool{"8.4": true}, "8.4")
	pool := renderSitePool(p.Sites[0], sitePoolTuning(2048, 1024, 1))
	for _, want := range []string{
		"[fastcp-site-alice-111111112222]",
		"user = alice",
		"listen.group = caddy",
		"listen.mode = 0660",
		"php_admin_value[open_basedir] = /home/alice/apps/example-com:/home/alice/.tmp:/tmp:/usr/share/php",
		"php_admin_value[error_log] = /home/alice/apps/example-com/logs/php-error.log",
	} {
		if !strings.Contains(pool, want) {
			t.Errorf("pool missing %q\n%s", want, pool)
		}
	}
}

func TestRenderCrontab(t *testing.T) {
	out, warnings := renderCrontab([]CronJob{
		{Name: "backup", Expression: "0 2 * * *", Command: "date +%Y-%m-%d >> ~/d.log", Enabled: true},
		{Name: "off", Expression: "* * * * *", Command: "true", Enabled: false},
		{Name: "evil", Expression: "* * * * *\n* * * * * root", Command: "id", Enabled: true},
		{Name: "evil2", Expression: "* * * * *", Command: "id\n* * * * * curl x", Enabled: true},
	})
	if !strings.Contains(out, `0 2 * * * date +\%Y-\%m-\%d >> ~/d.log`) {
		t.Errorf("percent not escaped:\n%s", out)
	}
	if strings.Contains(out, "root") || strings.Contains(out, "curl") || strings.Contains(out, "true") {
		t.Errorf("crontab contains disabled or injected entries:\n%s", out)
	}
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %v", warnings)
	}
}

func TestNormalizeCaddyConfig(t *testing.T) {
	c, err := normalizeCaddyConfig(CaddyConfig{Profile: "balanced", ReadHeader: "1h", AccessLogs: true}, 4096)
	if err != nil || c.ReadHeader != "10s" || !c.AccessLogs {
		t.Fatalf("preset must ignore custom timeouts: %+v %v", c, err)
	}
	if _, err := normalizeCaddyConfig(CaddyConfig{Profile: "custom", ReadHeader: "0s"}, 4096); err == nil {
		t.Fatal("accepted 0s timeout")
	}
	if _, err := normalizeCaddyConfig(CaddyConfig{Profile: "custom", ReadHeader: "10s\n}"}, 4096); err == nil {
		t.Fatal("accepted injected timeout")
	}
	if _, err := normalizeCaddyConfig(CaddyConfig{Profile: "turbo"}, 4096); err == nil {
		t.Fatal("accepted unknown profile")
	}
}

func TestPHPCatalog(t *testing.T) {
	vs := parseFPMPackages("php8.4-fpm - server-side, HTML-embedded scripting language (FPM-CGI binary)\nphp7.4-fpm - old\nphp8.10-fpm - future\nphp8.1-fpm\nphp-fpm - default\nphp8.4-fpm-dbgsym\n")
	if strings.Join(vs, ",") != "8.1,8.4,8.10" {
		t.Fatalf("parseFPMPackages = %v", vs)
	}
	if v := upstreamVersion("1:8.4.13-1+ubuntu24.04.1+deb.sury.org+1"); v != "8.4.13" {
		t.Fatalf("upstreamVersion = %q", v)
	}
	inst, cand := parseAptPolicy("php8.4-fpm:\n  Installed: 8.4.12-1+ubuntu24.04.1\n  Candidate: 8.4.13-1+ubuntu24.04.1\n  Version table:\n")
	if inst != "8.4.12-1+ubuntu24.04.1" || cand != "8.4.13-1+ubuntu24.04.1" {
		t.Fatalf("parseAptPolicy = %q %q", inst, cand)
	}
	if inst, _ := parseAptPolicy("Installed: (none)\nCandidate: 8.5.0-1"); inst != "" {
		t.Fatal("(none) not handled")
	}
	for deb, want := range map[string]bool{
		"8.6.0~rc3-1+ubuntu24.04.1+deb.sury.org+1": true,
		"1:8.6.0~beta1-1":                          true,
		"8.6.0RC1-1":                               true,
		"8.4.13-1+ubuntu24.04.1+deb.sury.org+1":    false,
		"8.4.13-1+0~20251010.70+debian12~1.gbp1":   false,
		"8.2.29-1~deb12u1":                         false,
		"8.3.6-0ubuntu0.24.04.5":                   false,
		"":                                         false,
	} {
		if got := isPrerelease(deb); got != want {
			t.Errorf("isPrerelease(%q) = %v", deb, got)
		}
	}
	released := releasedBranches([]string{"8.4", "8.5", "8.6", "8.7"}, map[string][2]string{
		"php8.4-fpm": {"", "8.4.13-1"},
		"php8.6-fpm": {"", "8.6.0~rc3-1"},
	})
	if strings.Join(released, ",") != "8.4,8.5" {
		t.Fatalf("releasedBranches = %v", released)
	}
	pk := parseInstalledPHPPackages("php8.4-fpm ii  8.4.13-1\nphp8.3-fpm rc  8.3.1-1\n")
	if pk["php8.4-fpm"] != "8.4.13-1" || pk["php8.3-fpm"] != "" {
		t.Fatalf("parseInstalledPHPPackages = %v", pk)
	}

	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cases := map[string]string{"8.1": "eol", "8.2": "security", "8.3": "security", "8.4": "active", "8.5": "active", "9.0": "active", "7.4": "unknown"}
	for v, want := range cases {
		if got, _ := phpSupportStatus(v, now); got != want {
			t.Errorf("phpSupportStatus(%s) = %s, want %s", v, got, want)
		}
	}
	if latestBranch([]string{"8.4", "8.10", "8.9"}) != "8.10" {
		t.Fatal("latestBranch wrong")
	}

	loc := time.FixedZone("x", 0)
	n := nextMaintenance(time.Date(2026, 1, 1, 4, 0, 0, 0, loc), 3, 17)
	if n.Day() != 2 || n.Hour() != 3 || n.Minute() != 17 {
		t.Fatalf("nextMaintenance = %v", n)
	}
}

func TestAutoInstallBranches(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	available := []string{"8.1", "8.2", "8.3", "8.4", "8.5"}
	for _, c := range []struct {
		installed []string
		want      string
	}{
		{nil, "8.5"},
		{[]string{"8.4"}, "8.5"},
		{[]string{"8.5"}, ""},
		{[]string{"8.2", "8.3"}, "8.4 8.5"},
	} {
		installed := map[string]bool{}
		for _, v := range c.installed {
			installed[v] = true
		}
		if got := strings.Join(autoInstallBranches(available, installed, now), " "); got != c.want {
			t.Errorf("installed %v: got %q, want %q", c.installed, got, c.want)
		}
	}
}

func TestSitePoolTuning(t *testing.T) {
	for _, c := range []struct{ ram, limit, sites, want int }{
		{24031, 4096, 16, 10},
		{24031, 4096, 1, 31},
		{24031, 0, 50, 31},
		{24031, 512, 4, 2},
		{24031, 128, 3, 1},
		{1024, 4096, 1, 2},
	} {
		if got := sitePoolTuning(c.ram, c.limit, c.sites).MaxChildren; got != c.want {
			t.Errorf("sitePoolTuning(%d, %d, %d) = %d workers, want %d", c.ram, c.limit, c.sites, got, c.want)
		}
	}
}

func TestRenderFPMIni(t *testing.T) {
	for _, c := range []struct {
		ram  int
		want []string
	}{
		{1024, []string{"opcache.memory_consumption = 128\n", "opcache.interned_strings_buffer = 8\n", "opcache.max_accelerated_files = 10000\n"}},
		{4096, []string{"opcache.memory_consumption = 256\n", "opcache.interned_strings_buffer = 16\n", "opcache.max_accelerated_files = 16384\n"}},
		{24031, []string{"opcache.memory_consumption = 1024\n", "opcache.interned_strings_buffer = 64\n", "opcache.max_accelerated_files = 65536\n"}},
	} {
		ini := renderFPMIni(c.ram)
		for _, want := range append(c.want, "expose_php = Off\n") {
			if !strings.Contains(ini, want) {
				t.Errorf("renderFPMIni(%d) missing %q\n%s", c.ram, want, ini)
			}
		}
	}
}

func TestSliceLimits(t *testing.T) {
	out := sliceLimits("alice", 1001, -1, 150)
	if !strings.Contains(out, "MemoryMax=infinity") || !strings.Contains(out, "CPUQuota=150%") {
		t.Fatal(out)
	}
	if out := sliceLimits("alice", 1001, 512, 0); !strings.Contains(out, "CPUQuota=\n") {
		t.Fatal(out)
	}
}
