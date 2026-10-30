package pagecache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

func adapt(t *testing.T, site string) (string, error) {
	t.Helper()
	out, _, err := caddyconfig.GetAdapter("caddyfile").Adapt([]byte(site), nil)
	return string(out), err
}

func TestCaddyfile(t *testing.T) {
	out, err := adapt(t, `example.com {
	root * /srv/www/public
	php_fastcgi unix//run/php/site.sock
	fastcp_page_cache
	file_server
}`)
	if err != nil {
		t.Fatal(err)
	}
	cache := strings.Index(out, `"handler":"fastcp_page_cache"`)
	if cache < 0 {
		t.Fatalf("no page cache handler in %s", out)
	}
	for _, later := range []string{`"handler":"rewrite"`, `"handler":"reverse_proxy"`, `"handler":"file_server"`} {
		if i := strings.Index(out, later); i < cache {
			t.Errorf("%s does not come after the page cache in %s", later, out)
		}
	}
	if i := strings.Index(out, `"handler":"vars"`); i < 0 || i > cache {
		t.Errorf("root is not set before the page cache in %s", out)
	}

	for _, bad := range []string{"fastcp_page_cache on", "fastcp_page_cache {\n\t\tttl 1\n\t}"} {
		if _, err := adapt(t, "example.com {\n\t"+bad+"\n}"); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestHandlerKeepsSiteHeaders(t *testing.T) {
	w := superCacheSite(t)
	rec := httptest.NewRecorder()
	rec.Header().Set("Cache-Control", "public, max-age=60")
	w.handle(rec, w.request(http.MethodGet, "/"))
	if rec.Body.String() != "home" || rec.Header().Get("Cache-Control") != "public, max-age=60" || rec.Header().Get("Vary") != "Accept-Encoding, Cookie" {
		t.Errorf("%q %v", rec.Body, rec.Header())
	}
}

// Requests php_fastcgi would not hand to WordPress's index.php, or not
// with this path, go on even when the plugin has a page saved for them.
func TestHandlerPassesOn(t *testing.T) {
	w := superCacheSite(t)
	cache := "wp-content/cache/supercache/example.com/"
	w.write("hello-world/index.php", "<?php\n")
	w.write("robots.txt", "User-agent: *\n")
	w.write(cache+"robots.txt/index-https.html", "robots")
	w.write(cache+"a*/index-https.html", "glob")
	w.write(cache+"a/b/index-https.html", "a/b")

	w.expect("/hello-world/", "") // a folder with its own index.php
	w.expect("/a*/", "")
	w.expect("/a//b/", "")
	w.expect("/../", "")

	w.setting("$wp_cache_slash_check = 1;", "$wp_cache_slash_check = 0;")
	w.expect("/hello-world", "") // Caddy redirects to the folder
	w.expect("/robots.txt", "")  // a file
	w.expect("/index.php", "")
	w.expect("/a/b", "a/b")

	// A path another handler rewrote reaches PHP as the original one.
	r := w.request(http.MethodGet, "/a/b")
	r.URL.Path = "/hello-world"
	rec := httptest.NewRecorder()
	w.handle(rec, r)
	if rec.Code != http.StatusTeapot {
		t.Errorf("rewritten path: %d %q", rec.Code, rec.Body)
	}

	// Without Caddy's vars there is nothing to go by.
	r = httptest.NewRequest(http.MethodGet, "https://example.com/a/b", nil)
	rec = httptest.NewRecorder()
	w.handle(rec, r)
	if rec.Code != http.StatusTeapot {
		t.Errorf("no vars: %d %q", rec.Code, rec.Body)
	}
}

func TestSettingsChanges(t *testing.T) {
	w := superCacheSite(t)
	e := &entry{}
	start := time.Now()
	first := e.current(osFS{}, w.root, start)
	if first.finder == nil {
		t.Fatalf("not serving: %q", first.status.Reasons)
	}
	if again := e.current(osFS{}, w.root, start.Add(2*time.Second)); again == first || again.finder != first.finder {
		t.Error("unchanged files were read again, or not rechecked")
	}

	config := w.path("wp-content/wp-cache-config.php")
	content, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(content), "$cache_enabled = true;", "$cache_enabled = 0;   ", 1)
	if err := os.WriteFile(config, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(config, start.Add(time.Minute), start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	s := e.current(osFS{}, w.root, start.Add(2500*time.Millisecond))
	if s.finder == nil {
		t.Error("a change was noticed before the next check")
	}
	s = e.current(osFS{}, w.root, start.Add(4*time.Second))
	if s.finder != nil || len(s.status.Reasons) == 0 || !strings.Contains(s.status.Reasons[0], "Caching is off") {
		t.Errorf("after the change: %+v", s.status)
	}
	if back := e.current(osFS{}, w.root, start); back == s {
		t.Error("a clock that went back did not force a check")
	}

	// Removing the drop-in turns serving off too.
	w.remove("wp-content/advanced-cache.php")
	if s := e.current(osFS{}, w.root, start.Add(time.Hour)); s.status.Plugin != "" || s.finder != nil {
		t.Errorf("after removing the drop-in: %+v", s.status)
	}
}
