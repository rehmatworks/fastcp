package pagecache

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// osFS opens absolute names, like Caddy's file systems.
type osFS struct{}

func (osFS) Open(name string) (fs.File, error) { return os.Open(name) }

type fsMap struct{ fsys fs.FS }

func (m fsMap) Register(string, fs.FS)     {}
func (m fsMap) Unregister(string)          {}
func (m fsMap) Get(k string) (fs.FS, bool) { return m.fsys, k == "site" }
func (m fsMap) Default() fs.FS             { return m.fsys }

func newHandler() *Handler {
	return &Handler{fsmap: fsMap{osFS{}}, sites: &sites{m: map[string]*entry{}}}
}

func header(name, value string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(name, value) }
}

// wordpressIndexPHP is WordPress's index.php.
const wordpressIndexPHP = `<?php
/**
 * Front to the WordPress application. This file doesn't do anything, but loads
 * wp-blog-header.php which does and tells WordPress to load the theme.
 *
 * @package WordPress
 */

/**
 * Tells WordPress to load the WordPress theme and output it.
 *
 * @var bool
 */
define( 'WP_USE_THEMES', true );

/** Loads the WordPress Environment and Template */
require __DIR__ . '/wp-blog-header.php';
`

const (
	desktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	iPhoneUA  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1"
)

// wp is a WordPress site on disk.
type wp struct {
	t    *testing.T
	root string
	h    *Handler
}

func newWP(t *testing.T) *wp {
	t.Helper()
	w := &wp{t: t, root: filepath.Join(t.TempDir(), "public"), h: newHandler()}
	w.write("index.php", wordpressIndexPHP)
	return w
}

func (w *wp) path(rel string) string { return filepath.Join(w.root, filepath.FromSlash(rel)) }

func (w *wp) write(rel, content string) {
	w.t.Helper()
	p := w.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
	w.h = newHandler()
}

func (w *wp) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(w.path(rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// edit replaces the first old in the file rel with new.
func (w *wp) edit(rel, old, new string) {
	w.t.Helper()
	content := w.read(rel)
	if !strings.Contains(content, old) {
		w.t.Fatalf("%s has no %q", rel, old)
	}
	w.write(rel, strings.Replace(content, old, new, 1))
}

func (w *wp) remove(rel string) {
	w.t.Helper()
	if err := os.RemoveAll(w.path(rel)); err != nil {
		w.t.Fatal(err)
	}
	w.h = newHandler()
}

func (w *wp) age(rel string, d time.Duration) {
	w.t.Helper()
	w.touch(rel, time.Now().Add(-d))
}

func (w *wp) touch(rel string, t time.Time) {
	w.t.Helper()
	if err := os.Chtimes(w.path(rel), t, t); err != nil {
		w.t.Fatal(err)
	}
}

// request is a browser-like request for target, a URL or a path on
// https://example.com, as Caddy hands it to the handler.
func (w *wp) request(method, target string, edits ...func(*http.Request)) *http.Request {
	if strings.HasPrefix(target, "/") {
		target = "https://example.com" + target
	}
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("User-Agent", desktopUA)
	r.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	r.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	for _, edit := range edits {
		edit(r)
	}
	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", w.root)
	repl.Set("http.vars.fs", "site")
	r = r.WithContext(context.WithValue(r.Context(), caddy.ReplacerCtxKey, repl))
	orig := *r.Clone(r.Context())
	return r.WithContext(context.WithValue(r.Context(), caddyhttp.OriginalRequestCtxKey, orig))
}

// handle runs the handler, with PHP answering 418.
func (w *wp) handle(rec *httptest.ResponseRecorder, r *http.Request) {
	w.t.Helper()
	next := caddyhttp.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) error {
		rw.Header().Set("X-Test", "php")
		rw.WriteHeader(http.StatusTeapot)
		return nil
	})
	if err := w.h.ServeHTTP(rec, r, next); err != nil {
		w.t.Fatal(err)
	}
}

func (w *wp) do(method, target string, edits ...func(*http.Request)) *httptest.ResponseRecorder {
	w.t.Helper()
	rec := httptest.NewRecorder()
	w.handle(rec, w.request(method, target, edits...))
	return rec
}

// served returns the cached page sent for a GET of target, or "" when the
// request went on to PHP.
func (w *wp) served(target string, edits ...func(*http.Request)) string {
	w.t.Helper()
	rec := w.do(http.MethodGet, target, edits...)
	if rec.Code == http.StatusTeapot {
		return ""
	}
	if rec.Code != http.StatusOK || rec.Header().Get("X-FastCP-Cache") != "HIT" {
		w.t.Fatalf("GET %s: status %d, headers %v", target, rec.Code, rec.Header())
	}
	return rec.Body.String()
}

func (w *wp) expect(target, want string, edits ...func(*http.Request)) {
	w.t.Helper()
	if got := w.served(target, edits...); got != want {
		if want == "" {
			w.t.Errorf("GET %s: sent %q, want PHP", target, got)
		} else if got == "" {
			w.t.Errorf("GET %s: went to PHP, want %q", target, want)
		} else {
			w.t.Errorf("GET %s: sent %q, want %q", target, got, want)
		}
	}
}

func (w *wp) status() Status {
	return loadSite(osFS{}, w.root, time.Now()).status
}

// wantReason checks the site is not served and a reason contains text.
func (w *wp) wantReason(text string) {
	w.t.Helper()
	st := w.status()
	if st.Serving {
		w.t.Fatalf("serving, want a reason containing %q", text)
	}
	for _, r := range st.Reasons {
		if strings.Contains(r, text) {
			return
		}
	}
	w.t.Fatalf("reasons %q do not mention %q", st.Reasons, text)
}

func (w *wp) wantServing() {
	w.t.Helper()
	if st := w.status(); !st.Serving {
		w.t.Fatalf("not serving: %q", st.Reasons)
	}
}
