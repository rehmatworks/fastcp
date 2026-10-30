// Package pagecache lets Caddy send the pages WP Super Cache and WP Rocket
// save to disk without starting PHP. It reads the plugin's own settings
// and follows the plugin's own serving code, so a page is sent from the
// cache only when the plugin's PHP would send that same file. Anything it
// cannot decide exactly, it leaves to PHP.
//
//	example.com {
//		root * /home/alice/apps/example/public
//		fs alice
//		fastcp_page_cache
//		php_fastcgi unix//run/php/alice-example.sock
//		file_server
//	}
package pagecache

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("fastcp_page_cache", parseCaddyfile)
	httpcaddyfile.RegisterDirectiveOrder("fastcp_page_cache", httpcaddyfile.Before, "php_fastcgi")
}

// recheck is how often a site's settings files are checked for changes.
const recheck = time.Second

// Handler sends a saved page when the site's cache plugin would, and
// passes every other request on.
type Handler struct {
	fsmap caddy.FileSystems
	sites *sites
}

type sites struct {
	mu sync.Mutex
	m  map[string]*entry
}

// entry holds what is known about one site.
type entry struct {
	mu   sync.Mutex
	site atomic.Pointer[site]
}

func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.fastcp_page_cache",
		New: func() caddy.Module { return new(Handler) },
	}
}

func (h *Handler) Provision(ctx caddy.Context) error {
	h.fsmap = ctx.FileSystems()
	h.sites = &sites{m: map[string]*entry{}}
	return nil
}

func parseCaddyfile(helper httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	h := new(Handler)
	return h, h.UnmarshalCaddyfile(helper.Dispenser)
}

// UnmarshalCaddyfile reads `fastcp_page_cache`, which takes no arguments.
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if d.NextArg() || d.NextBlock(0) {
		return d.ArgErr()
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	hit := h.lookup(r, time.Now())
	if hit == nil {
		return next.ServeHTTP(w, r)
	}
	defer hit.file.Close()
	hdr := w.Header()
	for _, kv := range hit.header {
		if _, set := hdr[kv[0]]; !set {
			hdr.Set(kv[0], kv[1])
		}
	}
	hdr.Set("Content-Type", "text/html; charset=UTF-8")
	hdr.Set("X-FastCP-Cache", "HIT")
	if hit.lastModified {
		mod := hit.info.ModTime().UTC().Truncate(time.Second)
		hdr.Set("Last-Modified", mod.Format(http.TimeFormat))
		if t, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && t.Equal(mod) {
			if _, set := hdr["Cache-Control"]; !set {
				hdr.Set("Cache-Control", "no-cache, must-revalidate")
			}
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
	}
	hdr.Set("Content-Length", strconv.FormatInt(hit.info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		io.CopyN(w, hit.file, hit.info.Size())
	}
	return nil
}

// lookup returns the saved page for r, or nil to pass r on to PHP.
func (h *Handler) lookup(r *http.Request, now time.Time) *hit {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil
	}
	repl, ok := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	if !ok {
		return nil
	}
	root := repl.ReplaceAll("{http.vars.root}", "")
	if !path.IsAbs(root) || path.Clean(root) != root || root == "/" {
		return nil
	}
	fsName := repl.ReplaceAll("{http.vars.fs}", "")
	fsys, ok := h.fsmap.Get(fsName)
	if !ok {
		return nil
	}
	// php_fastcgi gives PHP the original request's URI. Pages whose path
	// another handler changed, or with a query string, go to PHP.
	orig, ok := r.Context().Value(caddyhttp.OriginalRequestCtxKey).(http.Request)
	if !ok || orig.URL == nil || orig.URL.Path != r.URL.Path || orig.URL.RawPath != r.URL.RawPath ||
		orig.URL.RawQuery != "" || orig.URL.ForceQuery || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil
	}
	p := r.URL.Path
	if !cleanRequestPath(p) || strings.Contains(asciiLower(p), ".php") || strings.ContainsAny(p, `*?[\{}`) {
		return nil
	}
	s := h.entry(fsName, root).current(fsys, root, now)
	if s.finder == nil || !reachesWordPress(fsys, root, p) {
		return nil
	}
	return s.finder.find(fsys, newRequest(r, orig.URL.RequestURI()), now)
}

func (h *Handler) entry(fsName, root string) *entry {
	h.sites.mu.Lock()
	defer h.sites.mu.Unlock()
	key := fsName + "\x00" + root
	e := h.sites.m[key]
	if e == nil {
		e = &entry{}
		h.sites.m[key] = e
	}
	return e
}

// current returns what the site's files say, reading them again when
// one of them has changed. While one request does that, others use what
// was read before.
func (e *entry) current(fsys fs.FS, root string, now time.Time) *site {
	s := e.site.Load()
	if s != nil && now.Sub(s.checked) < recheck && !now.Before(s.checked) {
		return s
	}
	if s != nil {
		if !e.mu.TryLock() {
			return s
		}
	} else {
		e.mu.Lock()
	}
	defer e.mu.Unlock()
	if latest := e.site.Load(); latest != s {
		return latest
	}
	if s != nil && !s.changed(fsys) {
		fresh := *s
		fresh.checked = now
		e.site.Store(&fresh)
		return &fresh
	}
	s = loadSite(fsys, root, now)
	e.site.Store(s)
	return s
}

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
)
