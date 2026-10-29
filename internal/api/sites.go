package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	maxDomainsPerSite = 20
	phpMemoryMin      = 16 << 20
	phpSizeMin        = 1 << 20
	phpSizeMax        = 16 << 30
)

type siteView struct {
	*database.Site
	Domains []*database.SiteDomain `json:"domains"`
	SSL     map[string]sslStatus   `json:"ssl"`
}

func (s *Server) viewSite(site *database.Site, domains []*database.SiteDomain) siteView {
	if domains == nil {
		domains = []*database.SiteDomain{}
	}
	v := siteView{Site: site, Domains: domains, SSL: map[string]sslStatus{}}
	for _, d := range domains {
		if st, ok := s.ssl.get(d.Domain); ok {
			v.SSL[d.Domain] = st
		}
	}
	return v
}

// ownedSite loads the site in the URL if it belongs to the caller.
func (s *Server) ownedSite(r *http.Request) (*database.Site, error) {
	site, err := s.db.GetSite(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrNotFound) || (err == nil && site.Username != current(r).User.Username) {
		return nil, notFound("Site")
	}
	return site, err
}

func (s *Server) siteResponse(w http.ResponseWriter, r *http.Request, id string, status int) error {
	site, err := s.db.GetSite(r.Context(), id)
	if err != nil {
		return err
	}
	domains, err := s.db.ListDomains(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, status, s.viewSite(site, domains))
	return nil
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	sites, err := s.db.ListSites(ctx, current(r).User.Username)
	if err != nil {
		return err
	}
	domains, err := s.db.DomainsBySite(ctx)
	if err != nil {
		return err
	}
	out := make([]siteView, 0, len(sites))
	for _, site := range sites {
		out = append(out, s.viewSite(site, domains[site.ID]))
	}
	return ok(w, out)
}

func (s *Server) getSite(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}

// newDomain validates a domain for a site and checks it is free.
func (s *Server) newDomain(ctx context.Context, raw string) (string, error) {
	domain, err := validate.Domain(raw)
	if err != nil {
		return "", validation(err)
	}
	if host, err := os.Hostname(); err == nil && strings.Contains(host, ".") && strings.EqualFold(strings.TrimSuffix(host, "."), domain) {
		return "", badRequest("%s is this server's host name, which is reserved for the control panel.", domain)
	}
	taken, err := s.db.DomainTaken(ctx, domain)
	if err != nil {
		return "", err
	}
	if taken {
		return "", conflict("%s is already used by a site on this server.", domain)
	}
	return domain, nil
}

func (s *Server) checkDomain(w http.ResponseWriter, r *http.Request) error {
	domain, err := s.newDomain(r.Context(), r.URL.Query().Get("domain"))
	if err != nil {
		_, msg := classify(err)
		return ok(w, map[string]any{"available": false, "message": msg})
	}
	return ok(w, map[string]any{"available": true, "domain": domain})
}

func (s *Server) uniqueSlug(ctx context.Context, username, domain string) (string, error) {
	base := strings.NewReplacer(".", "_", "-", "_").Replace(domain)
	if len(base) > 56 {
		base = base[:56]
	}
	base = strings.TrimRight(base, "_")
	for i := 1; i < 1000; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s_%d", base, i)
		}
		if validate.Slug(slug) != nil {
			continue
		}
		taken, err := s.db.SlugTaken(ctx, username, slug)
		if err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
	}
	return "", conflict("Could not pick a directory name for %s.", domain)
}

// requirePHP checks that version is installed. An empty version picks the
// default.
func (s *Server) requirePHP(ctx context.Context, version string) (string, error) {
	installed, def, err := s.installedPHP(ctx)
	if err != nil {
		return "", err
	}
	if version == "" {
		version = def
	}
	if version == "" || !slices.Contains(installed, version) {
		if len(installed) == 0 {
			return "", badRequest("No PHP version is installed. An administrator can install one under Settings → PHP.")
		}
		return "", badRequest("PHP %s is not installed. Installed versions: %s.", version, strings.Join(installed, ", "))
	}
	return version, nil
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	ctx := r.Context()
	var req struct {
		Domain     string `json:"domain"`
		SiteType   string `json:"site_type"`
		PHPVersion string `json:"php_version"`
		ProxyURL   string `json:"proxy_url"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.SiteType == "" {
		req.SiteType = "php"
	}
	proxy := ""
	switch req.SiteType {
	case "php", "wordpress":
	case "proxy":
		u, err := validate.ProxyURL(req.ProxyURL, agent.ServerPorts...)
		if err != nil {
			return validation(err)
		}
		proxy = u
	default:
		return badRequest("Site type must be php, wordpress or proxy.")
	}
	domain, err := s.newDomain(ctx, req.Domain)
	if err != nil {
		return err
	}
	if p.User.MaxSites >= 0 {
		n, err := s.db.CountSites(ctx, p.User.Username)
		if err != nil {
			return err
		}
		if n >= p.User.MaxSites {
			return forbidden("This account is limited to %s.", plural(p.User.MaxSites, "site"))
		}
	}
	version := ""
	if proxy == "" {
		if version, err = s.requirePHP(ctx, req.PHPVersion); err != nil {
			return err
		}
	}
	slug, err := s.uniqueSlug(ctx, p.User.Username, domain)
	if err != nil {
		return err
	}

	site := &database.Site{
		ID: uuid.NewString(), Username: p.User.Username, Domain: domain, Slug: slug, SiteType: req.SiteType, ProxyURL: proxy,
		DocumentRoot:   "/home/" + p.User.Username + "/apps/" + slug + "/public",
		ForceHTTPSAuto: true, CompressionEnabled: true, GzipEnabled: true, ZstdEnabled: true,
		PHPVersion: version, PHPMemoryLimit: "256M", PHPPostMaxSize: "64M", PHPUploadMaxSize: "64M",
		PHPMaxExecutionTime: 300, PHPMaxInputVars: 5000,
	}
	agentSite := &agent.CreateSiteRequest{Username: site.Username, Slug: slug, Domain: domain}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.CreateSite(ctx, site); err != nil {
			if errors.Is(err, database.ErrConflict) {
				return nil, conflict("%s is already used by a site on this server.", domain)
			}
			return nil, err
		}
		if err := s.agent.CreateSite(ctx, agentSite); err != nil {
			_ = s.db.DeleteSite(ctx, site.ID)
			return nil, err
		}
		return func() {
			_ = s.db.DeleteSite(ctx, site.ID)
			s.removeSiteFiles(ctx, site)
		}, nil
	})
	if err != nil {
		return err
	}

	if site.SiteType == "wordpress" {
		if err := s.installWordPress(ctx, site); err != nil {
			slog.Warn("WordPress install failed; removing the site", "site", site.Domain, "error", err)
			if derr := s.removeSite(ctx, site); derr != nil {
				slog.Error("could not remove the half-created site", "site", site.Domain, "error", derr)
			}
			return err
		}
	}
	s.ssl.forget(domain)
	return s.siteResponse(w, r, site.ID, http.StatusCreated)
}

func (s *Server) installWordPress(ctx context.Context, site *database.Site) error {
	dbName, dbUser, err := s.newDatabaseNames(ctx, site.Username, "wp", true)
	if err != nil {
		return err
	}
	password := randomPassword(24)
	enc, err := s.box.Seal(crypto.PurposeDatabasePassword, password)
	if err != nil {
		return err
	}
	rec := &database.Database{ID: uuid.NewString(), Username: site.Username, DBName: dbName, DBUser: dbUser, PasswordEnc: enc}
	if err := s.db.CreateDatabase(ctx, rec); err != nil {
		return err
	}
	err = s.agent.InstallWordPress(ctx, &agent.InstallWordPressRequest{
		Username: site.Username, Slug: site.Slug, DBName: dbName, DBUser: dbUser, DBPass: password,
	})
	if err != nil {
		_ = s.agent.DeleteDatabase(ctx, &agent.DatabaseTarget{DBName: dbName, DBUser: dbUser})
		_ = s.db.DeleteDatabase(ctx, rec.ID)
		return err
	}
	return nil
}

// removeSite deletes a site from the database and the server.
func (s *Server) removeSite(ctx context.Context, site *database.Site) error {
	domains, err := s.db.ListDomains(ctx, site.ID)
	if err != nil {
		return err
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.DeleteSite(ctx, site.ID); err != nil {
			return nil, err
		}
		return func() {
			restored := *site
			if err := s.db.CreateSite(ctx, &restored); err != nil {
				slog.Error("could not restore site record", "site", site.Domain, "error", err)
				return
			}
			for _, d := range domains {
				if !d.IsPrimary {
					_ = s.db.AddDomain(ctx, &database.SiteDomain{SiteID: site.ID, Domain: d.Domain, RedirectToPrimary: d.RedirectToPrimary}, maxDomainsPerSite+1)
				}
			}
		}, nil
	})
	if err != nil {
		return err
	}
	s.removeSiteFiles(ctx, site)
	return nil
}

func (s *Server) removeSiteFiles(ctx context.Context, site *database.Site) {
	if err := s.agent.DeleteSite(ctx, &agent.DeleteSiteRequest{Username: site.Username, Slug: site.Slug}); err != nil {
		slog.Warn("could not remove site files", "site", site.Domain, "error", err)
	}
}

func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	domains, _ := s.db.ListDomains(r.Context(), site.ID)
	if err := s.removeSite(r.Context(), site); err != nil {
		return err
	}
	for _, d := range domains {
		s.ssl.forget(d.Domain)
	}
	return noContent(w)
}

type siteSettings struct {
	ProxyURL            *string `json:"proxy_url"`
	ForceHTTPS          *bool   `json:"force_https"`
	CompressionEnabled  *bool   `json:"compression_enabled"`
	GzipEnabled         *bool   `json:"gzip_enabled"`
	ZstdEnabled         *bool   `json:"zstd_enabled"`
	CacheControlEnabled *bool   `json:"cache_control_enabled"`
	CacheControlValue   *string `json:"cache_control_value"`
	PHPVersion          *string `json:"php_version"`
	PHPMemoryLimit      *string `json:"php_memory_limit"`
	PHPPostMaxSize      *string `json:"php_post_max_size"`
	PHPUploadMaxSize    *string `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime *int    `json:"php_max_execution_time"`
	PHPMaxInputVars     *int    `json:"php_max_input_vars"`
}

func phpSize(field, v string, lo int64) (string, error) {
	out, err := validate.PHPSize(v, lo, phpSizeMax)
	if err != nil {
		return "", badRequest("%s: %v", field, err)
	}
	return out, nil
}

func sizeBytes(v string) int64 {
	if v == "" {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimRight(v, "KMG"), 10, 64)
	switch v[len(v)-1] {
	case 'K':
		n <<= 10
	case 'M':
		n <<= 20
	case 'G':
		n <<= 30
	}
	return n
}

func (s *Server) applySiteSettings(ctx context.Context, site *database.Site, req *siteSettings) error {
	proxySite := site.SiteType == "proxy"
	if req.ProxyURL != nil {
		if !proxySite {
			return badRequest("Only reverse proxy sites forward requests to an application.")
		}
		u, err := validate.ProxyURL(*req.ProxyURL, agent.ServerPorts...)
		if err != nil {
			return validation(err)
		}
		site.ProxyURL = u
	}
	if proxySite && req.PHPVersion != nil {
		return badRequest("Reverse proxy sites do not run PHP.")
	}
	if req.ForceHTTPS != nil {
		site.ForceHTTPS = *req.ForceHTTPS
		site.ForceHTTPSAuto = false
	}
	if req.CompressionEnabled != nil {
		site.CompressionEnabled = *req.CompressionEnabled
	}
	if req.GzipEnabled != nil {
		site.GzipEnabled = *req.GzipEnabled
	}
	if req.ZstdEnabled != nil {
		site.ZstdEnabled = *req.ZstdEnabled
	}
	if site.CompressionEnabled && !site.GzipEnabled && !site.ZstdEnabled {
		return badRequest("Choose gzip, zstd or both, or turn compression off.")
	}
	if req.CacheControlEnabled != nil {
		site.CacheControlEnabled = *req.CacheControlEnabled
	}
	if req.CacheControlValue != nil {
		site.CacheControlValue = strings.TrimSpace(*req.CacheControlValue)
	}
	if site.CacheControlEnabled {
		v, err := validate.CacheControl(site.CacheControlValue)
		if err != nil {
			return validation(err)
		}
		site.CacheControlValue = v
	}
	if req.PHPVersion != nil && *req.PHPVersion != site.PHPVersion {
		v, err := s.requirePHP(ctx, *req.PHPVersion)
		if err != nil {
			return err
		}
		site.PHPVersion = v
	}
	var err error
	if req.PHPMemoryLimit != nil {
		if site.PHPMemoryLimit, err = phpSize("memory_limit", *req.PHPMemoryLimit, phpMemoryMin); err != nil {
			return err
		}
	}
	if req.PHPPostMaxSize != nil {
		if site.PHPPostMaxSize, err = phpSize("post_max_size", *req.PHPPostMaxSize, phpSizeMin); err != nil {
			return err
		}
	}
	if req.PHPUploadMaxSize != nil {
		if site.PHPUploadMaxSize, err = phpSize("upload_max_filesize", *req.PHPUploadMaxSize, phpSizeMin); err != nil {
			return err
		}
	}
	if sizeBytes(site.PHPPostMaxSize) < sizeBytes(site.PHPUploadMaxSize) {
		return badRequest("post_max_size must be at least as large as upload_max_filesize.")
	}
	if req.PHPMaxExecutionTime != nil {
		if v := *req.PHPMaxExecutionTime; v < 1 || v > 3600 {
			return badRequest("max_execution_time must be between 1 and 3600 seconds.")
		}
		site.PHPMaxExecutionTime = *req.PHPMaxExecutionTime
	}
	if req.PHPMaxInputVars != nil {
		if v := *req.PHPMaxInputVars; v < 100 || v > 100000 {
			return badRequest("max_input_vars must be between 100 and 100000.")
		}
		site.PHPMaxInputVars = *req.PHPMaxInputVars
	}
	return nil
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	var req siteSettings
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	before := *site
	if err := s.applySiteSettings(ctx, site, &req); err != nil {
		return err
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.UpdateSite(ctx, site); err != nil {
			return nil, err
		}
		return func() { _ = s.db.UpdateSite(ctx, &before) }, nil
	})
	if err != nil {
		return err
	}
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}

func domainID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "domainID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, notFound("Domain")
	}
	return id, nil
}

func (s *Server) addDomain(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	var req struct {
		Domain            string `json:"domain"`
		RedirectToPrimary bool   `json:"redirect_to_primary"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	domain, err := s.newDomain(ctx, req.Domain)
	if err != nil {
		return err
	}
	d := &database.SiteDomain{SiteID: site.ID, Domain: domain, RedirectToPrimary: req.RedirectToPrimary}
	err = s.change(ctx, func() (func(), error) {
		switch err := s.db.AddDomain(ctx, d, maxDomainsPerSite); {
		case errors.Is(err, database.ErrConflict):
			return nil, conflict("%s is already used by a site on this server.", domain)
		case errors.Is(err, database.ErrLimit):
			return nil, badRequest("A site can have at most %d domains.", maxDomainsPerSite)
		case err != nil:
			return nil, err
		}
		return func() { _ = s.db.DeleteDomain(ctx, site.ID, d.ID) }, nil
	})
	if err != nil {
		return err
	}
	s.ssl.forget(domain)
	return s.siteResponse(w, r, site.ID, http.StatusCreated)
}

func (s *Server) updateDomain(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	id, err := domainID(r)
	if err != nil {
		return err
	}
	var req struct {
		RedirectToPrimary bool `json:"redirect_to_primary"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	d, err := s.db.GetDomain(ctx, site.ID, id)
	if err != nil {
		return err
	}
	if d.IsPrimary {
		return badRequest("The primary domain cannot redirect to itself.")
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.SetDomainRedirect(ctx, id, req.RedirectToPrimary); err != nil {
			return nil, err
		}
		return func() { _ = s.db.SetDomainRedirect(ctx, id, d.RedirectToPrimary) }, nil
	})
	if err != nil {
		return err
	}
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}

func (s *Server) setPrimaryDomain(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	id, err := domainID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := s.db.GetDomain(ctx, site.ID, id); err != nil {
		return err
	}
	domains, err := s.db.ListDomains(ctx, site.ID)
	if err != nil {
		return err
	}
	var old *database.SiteDomain
	for _, d := range domains {
		if d.IsPrimary {
			old = d
		}
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.SetPrimaryDomain(ctx, site.ID, id); err != nil {
			return nil, err
		}
		return func() {
			if old != nil {
				_ = s.db.SetPrimaryDomain(ctx, site.ID, old.ID)
			}
		}, nil
	})
	if err != nil {
		return err
	}
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}

func (s *Server) deleteDomain(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	id, err := domainID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	d, err := s.db.GetDomain(ctx, site.ID, id)
	if err != nil {
		return err
	}
	if d.IsPrimary {
		return badRequest("Make another domain primary before removing %s.", d.Domain)
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.DeleteDomain(ctx, site.ID, id); err != nil {
			return nil, err
		}
		return func() {
			_ = s.db.AddDomain(ctx, &database.SiteDomain{SiteID: site.ID, Domain: d.Domain, RedirectToPrimary: d.RedirectToPrimary}, maxDomainsPerSite+1)
		}, nil
	})
	if err != nil {
		return err
	}
	s.ssl.forget(d.Domain)
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}

func (s *Server) recheckSSL(w http.ResponseWriter, r *http.Request) error {
	site, err := s.ownedSite(r)
	if err != nil {
		return err
	}
	domains, err := s.db.ListDomains(r.Context(), site.ID)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(domains))
	for _, d := range domains {
		names = append(names, d.Domain)
	}
	s.ssl.checkAll(r.Context(), names)
	s.autoForceHTTPS(r.Context(), site, domains)
	return s.siteResponse(w, r, site.ID, http.StatusOK)
}
