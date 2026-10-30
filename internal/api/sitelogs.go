package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/netip"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/agent"
)

// siteLogLines caps the lines or requests returned for one log.
const siteLogLines = 1000

type accessEntry struct {
	Time       time.Time `json:"time"`
	IP         string    `json:"ip"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	URI        string    `json:"uri"`
	Proto      string    `json:"proto"`
	Status     int       `json:"status"`
	Size       int64     `json:"size"`
	DurationMS float64   `json:"duration_ms"`
	UserAgent  string    `json:"user_agent,omitempty"`
	Referer    string    `json:"referer,omitempty"`
}

type siteLogView struct {
	Kind      string     `json:"kind"`
	Path      string     `json:"path"`
	Enabled   bool       `json:"enabled"`
	Exists    bool       `json:"exists"`
	Size      int64      `json:"size"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Truncated bool       `json:"truncated"`
	// Lines is the PHP error log, oldest first.
	Lines []string `json:"lines"`
	// Entries are access log requests, newest first.
	Entries []accessEntry `json:"entries"`
}

func (s *Server) siteLog(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	kind := chi.URLParam(r, "kind")
	view := siteLogView{Kind: kind, Enabled: true, Lines: []string{}, Entries: []accessEntry{}}
	switch kind {
	case agent.SiteLogPHP:
		view.Path = path.Join("~/apps", site.Slug, "logs", "php-error.log")
	case agent.SiteLogAccess:
		cfg, err := s.agent.GetCaddyConfig(r.Context())
		if err != nil {
			return err
		}
		view.Enabled = cfg.AccessLogs
		view.Path = path.Join(agent.SiteLogRoot, site.Username, site.Slug, "access.log")
	default:
		return notFound("Log")
	}
	res, err := s.agent.ReadSiteLog(r.Context(), &agent.SiteLogRequest{Username: site.Username, Slug: site.Slug, Kind: kind})
	if err != nil {
		return err
	}
	view.Exists, view.Size, view.Truncated = res.Exists, res.Size, res.Truncated
	if res.Exists {
		view.UpdatedAt = &res.ModTime
	}
	var lines []string
	if data := strings.TrimRight(res.Data, "\n"); data != "" {
		lines = strings.Split(data, "\n")
	}
	if len(lines) > siteLogLines {
		lines, view.Truncated = lines[len(lines)-siteLogLines:], true
	}
	if kind == agent.SiteLogPHP {
		view.Lines = append(view.Lines, lines...)
		return ok(w, view)
	}
	for _, line := range slices.Backward(lines) {
		if e, parsed := parseAccessLine(line); parsed {
			view.Entries = append(view.Entries, e)
		}
	}
	return ok(w, view)
}

// sitePageCache reports whether the web server sends the site's saved
// WordPress pages itself, whether or not the site has that turned on.
func (s *Server) sitePageCache(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	if site.SiteType == "proxy" {
		return ok(w, agent.PageCacheStatus{})
	}
	st, err := s.agent.PageCacheStatus(r.Context(), &agent.PageCacheRequest{Username: site.Username, Slug: site.Slug})
	if err != nil {
		return err
	}
	return ok(w, st)
}

// parseAccessLine reads one request from Caddy's JSON access log.
func parseAccessLine(line string) (accessEntry, bool) {
	var raw struct {
		TS      float64 `json:"ts"`
		Request struct {
			RemoteIP string      `json:"remote_ip"`
			ClientIP string      `json:"client_ip"`
			Proto    string      `json:"proto"`
			Method   string      `json:"method"`
			Host     string      `json:"host"`
			URI      string      `json:"uri"`
			Headers  http.Header `json:"headers"`
		} `json:"request"`
		Duration float64 `json:"duration"`
		Size     int64   `json:"size"`
		Status   int     `json:"status"`
	}
	if json.Unmarshal([]byte(line), &raw) != nil || raw.Request.Method == "" {
		return accessEntry{}, false
	}
	req := raw.Request
	ip := req.ClientIP
	if ip == "" {
		ip = req.RemoteIP
	}
	// Behind Cloudflare the connection comes from its edge; the visitor's
	// address is in a header that only counts when Cloudflare sent it.
	if addr, err := netip.ParseAddr(req.RemoteIP); err == nil && isCloudflare(addr) {
		if v, err := netip.ParseAddr(req.Headers.Get("CF-Connecting-IP")); err == nil {
			ip = v.String()
		}
	}
	return accessEntry{
		Time:       time.UnixMilli(int64(math.Round(raw.TS * 1000))).UTC(),
		IP:         ip,
		Method:     req.Method,
		Host:       req.Host,
		URI:        req.URI,
		Proto:      req.Proto,
		Status:     raw.Status,
		Size:       raw.Size,
		DurationMS: math.Round(raw.Duration*1e4) / 10,
		UserAgent:  req.Headers.Get("User-Agent"),
		Referer:    req.Headers.Get("Referer"),
	}, true
}
