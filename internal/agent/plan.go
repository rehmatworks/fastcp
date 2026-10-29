package agent

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/rehmatworks/fastcp/internal/validate"
)

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tenant is a resolved system account that may own sites.
type tenant struct {
	Name string
	UID  int
	GID  int
	Home string
}

var siteIDRE = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Bounds for per-site PHP settings.
const (
	phpMemoryMin = 16 << 20
	phpMemoryMax = 16 << 30
	phpSizeMin   = 1 << 20
	phpSizeMax   = 16 << 30
)

type sitePlan struct {
	StateSite
	Tenant     *tenant
	Suspended  bool
	SiteDir    string
	DocRoot    string
	LogDir     string
	PHPVersion string // empty when no PHP runtime is available
	Socket     string
	PoolName   string
	Primary    string
	Serving    []string
	Redirects  []string
}

type plan struct {
	Sites    []*sitePlan
	Users    map[string]*StateUser
	Tenants  map[string]*tenant
	Warnings []string
}

func (p *plan) warnf(format string, a ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, a...))
}

func socketID(siteID string) string {
	id := strings.ToLower(strings.ReplaceAll(siteID, "-", ""))
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func phpTag(version string) string { return strings.ReplaceAll(version, ".", "") }

func sitePoolName(username, siteID string) string {
	return "fastcp-site-" + username + "-" + socketID(siteID)
}

func siteSocketPath(username, siteID, version string) string {
	return path.Join(PHPSocketRoot, username, socketID(siteID)+"-php"+phpTag(version)+".sock")
}

func sitePoolFile(version, pool string) string {
	return path.Join("/etc/php", version, "fpm/pool.d", pool+".conf")
}

func pmaSocketPath() string { return path.Join(PHPSocketRoot, "phpmyadmin.sock") }

// buildPlan validates the pushed state against the system. Invalid entries
// are dropped with a warning so one bad record cannot take every site down.
// installed lists PHP versions with a working FPM; defaultPHP is used when a
// site's version is unavailable.
func buildPlan(st *State, lookup func(string) (*tenant, error), installed map[string]bool, defaultPHP string) *plan {
	p := &plan{Users: map[string]*StateUser{}, Tenants: map[string]*tenant{}}
	for i := range st.Users {
		u := &st.Users[i]
		if err := validate.TenantUsername(u.Username); err != nil {
			p.warnf("user %q skipped: %v", u.Username, err)
			continue
		}
		t, err := lookup(u.Username)
		if err != nil {
			p.warnf("user %q skipped: %v", u.Username, err)
			continue
		}
		p.Users[u.Username] = u
		p.Tenants[u.Username] = t
	}

	claimed := map[string]string{}
	seenIDs := map[string]bool{}
	sockets := map[string]string{}
	sites := append([]StateSite(nil), st.Sites...)
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	for _, s := range sites {
		label := s.Username + "/" + s.Slug
		if !siteIDRE.MatchString(s.ID) || seenIDs[s.ID] {
			p.warnf("site %s skipped: invalid or duplicate id", label)
			continue
		}
		seenIDs[s.ID] = true
		t := p.Tenants[s.Username]
		if t == nil {
			p.warnf("site %s skipped: owner is not a registered tenant", label)
			continue
		}
		if err := validate.Slug(s.Slug); err != nil {
			p.warnf("site %s skipped: %v", label, err)
			continue
		}
		sid := s.Username + "/" + socketID(s.ID)
		if other, dup := sockets[sid]; dup {
			p.warnf("site %s skipped: id collides with %s", label, other)
			continue
		}
		sockets[sid] = label
		if s.ProxyURL != "" {
			u, err := validate.ProxyURL(s.ProxyURL, ServerPorts...)
			if err != nil {
				p.warnf("site %s skipped: %v", label, err)
				continue
			}
			s.ProxyURL = u
		}

		sp := &sitePlan{StateSite: s, Tenant: t, Suspended: p.Users[s.Username].Suspended}
		sp.SiteDir = path.Join(t.Home, "apps", s.Slug)
		sp.DocRoot = path.Join(sp.SiteDir, "public")
		sp.LogDir = path.Join(SiteLogRoot, s.Username, s.Slug)
		sp.PoolName = sitePoolName(s.Username, s.ID)

		var domains []StateDomain
		for _, d := range s.Domains {
			name, err := validate.Domain(d.Domain)
			if err != nil {
				p.warnf("site %s: domain %q dropped: %v", label, d.Domain, err)
				continue
			}
			if owner, taken := claimed[name]; taken {
				p.warnf("site %s: domain %s dropped: already used by %s", label, name, owner)
				continue
			}
			claimed[name] = label
			d.Domain = name
			domains = append(domains, d)
		}
		if len(domains) == 0 {
			p.warnf("site %s skipped: no valid domains", label)
			continue
		}
		primary := -1
		for i, d := range domains {
			if d.Primary {
				primary = i
				break
			}
		}
		if primary < 0 {
			primary = 0
		}
		sp.Primary = domains[primary].Domain
		for i, d := range domains {
			if i != primary && d.RedirectToPrimary {
				sp.Redirects = append(sp.Redirects, d.Domain)
			} else {
				sp.Serving = append(sp.Serving, d.Domain)
			}
		}

		if s.ProxyURL == "" {
			version := s.PHPVersion
			if validate.PHPVersion(version) != nil || !installed[version] {
				if installed[defaultPHP] {
					if version != "" {
						p.warnf("site %s: PHP %s is not installed, using PHP %s", label, version, defaultPHP)
					}
					version = defaultPHP
				} else {
					p.warnf("site %s: no PHP runtime is installed, serving static files only", label)
					version = ""
				}
			}
			sp.PHPVersion = version
			if version != "" {
				sp.Socket = siteSocketPath(s.Username, s.ID, version)
			}
		}

		sp.PHPMemoryLimit = phpSizeOr(p, label, "memory_limit", s.PHPMemoryLimit, "256M", phpMemoryMin, phpMemoryMax)
		sp.PHPPostMaxSize = phpSizeOr(p, label, "post_max_size", s.PHPPostMaxSize, "64M", phpSizeMin, phpSizeMax)
		sp.PHPUploadMaxSize = phpSizeOr(p, label, "upload_max_filesize", s.PHPUploadMaxSize, "64M", phpSizeMin, phpSizeMax)
		sp.PHPMaxExecutionTime = intOr(p, label, "max_execution_time", s.PHPMaxExecutionTime, 300, 1, 86400)
		sp.PHPMaxInputVars = intOr(p, label, "max_input_vars", s.PHPMaxInputVars, 5000, 100, 1000000)

		if s.CacheControl != "" {
			cc, err := validate.CacheControl(s.CacheControl)
			if err != nil {
				p.warnf("site %s: Cache-Control header dropped: %v", label, err)
				cc = ""
			}
			sp.CacheControl = cc
		}
		p.Sites = append(p.Sites, sp)
	}
	return p
}

func phpSizeOr(p *plan, label, name, v, def string, min, max int64) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	out, err := validate.PHPSize(v, min, max)
	if err != nil {
		p.warnf("site %s: %s reset to %s: %v", label, name, def, err)
		return def
	}
	return out
}

func intOr(p *plan, label, name string, v, def, min, max int) int {
	if v == 0 {
		return def
	}
	if v < min || v > max {
		p.warnf("site %s: %s reset to %d (allowed %d-%d)", label, name, def, min, max)
		return def
	}
	return v
}

// tenantSiteCounts counts active sites per user, for FPM pool sizing.
func (p *plan) tenantSiteCounts() map[string]int {
	out := map[string]int{}
	for _, s := range p.Sites {
		if !s.Suspended {
			out[s.Username]++
		}
	}
	return out
}
