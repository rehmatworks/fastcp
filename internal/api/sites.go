package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
var slugRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)
var phpSizeRegex = regexp.MustCompile(`(?i)^\s*(\d+)\s*([kmg])?\s*$`)

// SiteService handles site operations
type SiteService struct {
	db    *database.DB
	agent *agent.Client
}

// NewSiteService creates a new site service
func NewSiteService(db *database.DB, agent *agent.Client) *SiteService {
	return &SiteService{db: db, agent: agent}
}

func isLocalDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	return d == "localhost" ||
		d == "127.0.0.1" ||
		d == "::1" ||
		strings.HasSuffix(d, ".localhost") ||
		strings.HasSuffix(d, ".local") ||
		strings.HasSuffix(d, ".test")
}

type sslEvaluation struct {
	valid  bool
	status string
	reason string
}

func sslStatusFromDialError(err error) (string, string) {
	errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(errMsg, "no such host"):
		return "dns_error", "DNS record not found for this domain."
	case strings.Contains(errMsg, "i/o timeout"):
		return "timeout", "TLS handshake timed out. Check DNS and reachability on port 443."
	case strings.Contains(errMsg, "connection refused"):
		return "unreachable", "Port 443 refused the connection."
	case strings.Contains(errMsg, "certificate is valid for"):
		return "cert_mismatch", "A certificate was found, but it does not match this domain."
	case strings.Contains(errMsg, "unknown authority"):
		return "untrusted_cert", "A certificate exists but is not trusted yet."
	case strings.Contains(errMsg, "tls: handshake failure"), strings.Contains(errMsg, "remote error: tls"):
		return "handshake_failed", "TLS handshake failed while validating certificate."
	default:
		return "pending", "Certificate not ready yet."
	}
}

func (s *SiteService) evaluateSSL(domain string, sslEnabled bool) sslEvaluation {
	if !sslEnabled || isLocalDomain(domain) {
		if !sslEnabled {
			return sslEvaluation{
				valid:  false,
				status: "disabled",
				reason: "SSL checks are disabled for this site.",
			}
		}
		return sslEvaluation{
			valid:  false,
			status: "local_domain",
			reason: "Local/test domains do not receive public CA certificates.",
		}
	}
	domain = strings.TrimSpace(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return sslEvaluation{
			valid:  false,
			status: "invalid_domain",
			reason: "Domain is missing or invalid.",
		}
	}

	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(domain, "443"), &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		status, reason := sslStatusFromDialError(err)
		return sslEvaluation{
			valid:  false,
			status: status,
			reason: reason,
		}
	}
	defer conn.Close()
	return sslEvaluation{
		valid:  true,
		status: "valid",
		reason: "SSL certificate is issued and trusted.",
	}
}

func shouldAutoEnableForceHTTPS(dbSite *database.Site, dbDomains []*database.SiteDomain, eval func(string, bool) sslEvaluation) bool {
	if dbSite == nil || dbSite.ForceHTTPS || !dbSite.SSLEnabled {
		return false
	}

	var servingDomains []string
	for _, d := range dbDomains {
		if d == nil || d.RedirectToPrimary {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(d.Domain))
		if domain != "" {
			servingDomains = append(servingDomains, domain)
		}
	}
	if len(servingDomains) == 0 {
		domain := strings.ToLower(strings.TrimSpace(dbSite.Domain))
		if domain != "" {
			servingDomains = append(servingDomains, domain)
		}
	}

	// Only auto-enable for public domains. Local/test-only sites should stay manual.
	hasPublicDomain := false
	for _, domain := range servingDomains {
		if !isLocalDomain(domain) {
			hasPublicDomain = true
			break
		}
	}
	if !hasPublicDomain {
		return false
	}

	for _, domain := range servingDomains {
		if isLocalDomain(domain) {
			continue
		}
		if !eval(domain, dbSite.SSLEnabled).valid {
			return false
		}
	}
	return true
}

func (s *SiteService) applyForceHTTPSEnabled(ctx context.Context, dbSite *database.Site) error {
	return s.db.UpdateSiteSettings(
		ctx,
		dbSite.ID,
		dbSite.Username,
		true,
		dbSite.CompressionEnabled,
		dbSite.GzipEnabled,
		dbSite.ZstdEnabled,
		dbSite.CacheControlEnabled,
		dbSite.CacheControlValue,
		dbSite.PHPVersion,
		dbSite.PHPMemoryLimit,
		dbSite.PHPPostMaxSize,
		dbSite.PHPUploadMaxSize,
		dbSite.PHPMaxExecutionTime,
		dbSite.PHPMaxInputVars,
	)
}

func normalizePHPSizeSetting(raw, fallback string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		v = strings.TrimSpace(fallback)
	}
	match := phpSizeRegex.FindStringSubmatch(v)
	if len(match) != 3 {
		return "", fmt.Errorf("invalid size format: %q (use values like 256M, 64M, 1G)", v)
	}
	numberPart := strings.TrimSpace(match[1])
	unitPart := strings.ToUpper(strings.TrimSpace(match[2]))
	if unitPart == "" {
		return numberPart, nil
	}
	return numberPart + unitPart, nil
}

func parsePHPSizeBytes(value string) (int64, error) {
	match := phpSizeRegex.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 3 {
		return 0, fmt.Errorf("invalid size: %q", value)
	}
	n, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size number: %q", value)
	}
	switch strings.ToUpper(strings.TrimSpace(match[2])) {
	case "G":
		n *= 1024 * 1024 * 1024
	case "M":
		n *= 1024 * 1024
	case "K":
		n *= 1024
	}
	return n, nil
}

// List returns all sites for a user
func (s *SiteService) List(ctx context.Context, username string) ([]*Site, error) {
	dbSites, err := s.db.ListSites(ctx, username)
	if err != nil {
		return nil, err
	}

	sites := make([]*Site, len(dbSites))
	autoForcedAny := false
	for i, dbSite := range dbSites {
		// Get domains for this site
		dbDomains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)
		if shouldAutoEnableForceHTTPS(dbSite, dbDomains, s.evaluateSSL) {
			if err := s.applyForceHTTPSEnabled(ctx, dbSite); err == nil {
				dbSite.ForceHTTPS = true
				autoForcedAny = true
			}
		}
		ssl := s.evaluateSSL(dbSite.Domain, dbSite.SSLEnabled)
		sites[i] = &Site{
			ID:                  dbSite.ID,
			Username:            dbSite.Username,
			Domain:              dbSite.Domain,
			Slug:                dbSite.Slug,
			SiteType:            dbSite.SiteType,
			DocumentRoot:        dbSite.DocumentRoot,
			SSLEnabled:          dbSite.SSLEnabled,
			SSLValid:            ssl.valid,
			SSLStatus:           ssl.status,
			SSLReason:           ssl.reason,
			ForceHTTPS:          dbSite.ForceHTTPS,
			CompressionEnabled:  dbSite.CompressionEnabled,
			GzipEnabled:         dbSite.GzipEnabled,
			ZstdEnabled:         dbSite.ZstdEnabled,
			CacheControlEnabled: dbSite.CacheControlEnabled,
			CacheControlValue:   dbSite.CacheControlValue,
			PHPVersion:          dbSite.PHPVersion,
			PHPMemoryLimit:      dbSite.PHPMemoryLimit,
			PHPPostMaxSize:      dbSite.PHPPostMaxSize,
			PHPUploadMaxSize:    dbSite.PHPUploadMaxSize,
			PHPMaxExecutionTime: dbSite.PHPMaxExecutionTime,
			PHPMaxInputVars:     dbSite.PHPMaxInputVars,
			CreatedAt:           dbSite.CreatedAt,
			Domains:             convertDomains(dbDomains),
		}
	}
	if autoForcedAny {
		if err := s.agent.ReloadCaddy(ctx); err != nil {
			fmt.Printf("warning: failed to reload Caddy after auto-enabling force HTTPS: %v\n", err)
		}
	}
	return sites, nil
}

// ListPaginated returns paginated sites with search
func (s *SiteService) ListPaginated(ctx context.Context, username string, page, limit int, search string) ([]*Site, int, error) {
	dbSites, total, err := s.db.ListSitesPaginated(ctx, username, page, limit, search)
	if err != nil {
		return nil, 0, err
	}

	sites := make([]*Site, len(dbSites))
	autoForcedAny := false
	for i, dbSite := range dbSites {
		dbDomains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)
		if shouldAutoEnableForceHTTPS(dbSite, dbDomains, s.evaluateSSL) {
			if err := s.applyForceHTTPSEnabled(ctx, dbSite); err == nil {
				dbSite.ForceHTTPS = true
				autoForcedAny = true
			}
		}
		ssl := s.evaluateSSL(dbSite.Domain, dbSite.SSLEnabled)
		sites[i] = &Site{
			ID:                  dbSite.ID,
			Username:            dbSite.Username,
			Domain:              dbSite.Domain,
			Slug:                dbSite.Slug,
			SiteType:            dbSite.SiteType,
			DocumentRoot:        dbSite.DocumentRoot,
			SSLEnabled:          dbSite.SSLEnabled,
			SSLValid:            ssl.valid,
			SSLStatus:           ssl.status,
			SSLReason:           ssl.reason,
			ForceHTTPS:          dbSite.ForceHTTPS,
			CompressionEnabled:  dbSite.CompressionEnabled,
			GzipEnabled:         dbSite.GzipEnabled,
			ZstdEnabled:         dbSite.ZstdEnabled,
			CacheControlEnabled: dbSite.CacheControlEnabled,
			CacheControlValue:   dbSite.CacheControlValue,
			PHPVersion:          dbSite.PHPVersion,
			PHPMemoryLimit:      dbSite.PHPMemoryLimit,
			PHPPostMaxSize:      dbSite.PHPPostMaxSize,
			PHPUploadMaxSize:    dbSite.PHPUploadMaxSize,
			PHPMaxExecutionTime: dbSite.PHPMaxExecutionTime,
			PHPMaxInputVars:     dbSite.PHPMaxInputVars,
			CreatedAt:           dbSite.CreatedAt,
			Domains:             convertDomains(dbDomains),
		}
	}
	if autoForcedAny {
		if err := s.agent.ReloadCaddy(ctx); err != nil {
			fmt.Printf("warning: failed to reload Caddy after auto-enabling force HTTPS: %v\n", err)
		}
	}
	return sites, total, nil
}

func convertDomains(dbDomains []*database.SiteDomain) []SiteDomain {
	if dbDomains == nil {
		return nil
	}
	domains := make([]SiteDomain, len(dbDomains))
	for i, d := range dbDomains {
		domains[i] = SiteDomain{
			ID:                d.ID,
			SiteID:            d.SiteID,
			Domain:            d.Domain,
			IsPrimary:         d.IsPrimary,
			RedirectToPrimary: d.RedirectToPrimary,
			CreatedAt:         d.CreatedAt,
		}
	}
	return domains
}

// Get returns a single site
func (s *SiteService) Get(ctx context.Context, id, username string) (*Site, error) {
	dbSite, err := s.db.GetSite(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check ownership
	if dbSite.Username != username {
		return nil, fmt.Errorf("site not found")
	}

	// Get domains for this site
	dbDomains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)
	if shouldAutoEnableForceHTTPS(dbSite, dbDomains, s.evaluateSSL) {
		if err := s.applyForceHTTPSEnabled(ctx, dbSite); err == nil {
			dbSite.ForceHTTPS = true
			if reloadErr := s.agent.ReloadCaddy(ctx); reloadErr != nil {
				fmt.Printf("warning: failed to reload Caddy after auto-enabling force HTTPS: %v\n", reloadErr)
			}
		}
	}
	ssl := s.evaluateSSL(dbSite.Domain, dbSite.SSLEnabled)

	return &Site{
		ID:                  dbSite.ID,
		Username:            dbSite.Username,
		Domain:              dbSite.Domain,
		Slug:                dbSite.Slug,
		SiteType:            dbSite.SiteType,
		DocumentRoot:        dbSite.DocumentRoot,
		SSLEnabled:          dbSite.SSLEnabled,
		SSLValid:            ssl.valid,
		SSLStatus:           ssl.status,
		SSLReason:           ssl.reason,
		ForceHTTPS:          dbSite.ForceHTTPS,
		CompressionEnabled:  dbSite.CompressionEnabled,
		GzipEnabled:         dbSite.GzipEnabled,
		ZstdEnabled:         dbSite.ZstdEnabled,
		CacheControlEnabled: dbSite.CacheControlEnabled,
		CacheControlValue:   dbSite.CacheControlValue,
		PHPVersion:          dbSite.PHPVersion,
		PHPMemoryLimit:      dbSite.PHPMemoryLimit,
		PHPPostMaxSize:      dbSite.PHPPostMaxSize,
		PHPUploadMaxSize:    dbSite.PHPUploadMaxSize,
		PHPMaxExecutionTime: dbSite.PHPMaxExecutionTime,
		PHPMaxInputVars:     dbSite.PHPMaxInputVars,
		CreatedAt:           dbSite.CreatedAt,
		Domains:             convertDomains(dbDomains),
	}, nil
}

// Create creates a new site
func (s *SiteService) Create(ctx context.Context, req *CreateSiteRequest) (*Site, error) {
	// Check user's site limit
	user, err := s.db.GetUser(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if user.MaxSites != -1 {
		siteCount, err := s.db.CountUserSites(ctx, req.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to count sites: %w", err)
		}
		if siteCount >= user.MaxSites {
			return nil, fmt.Errorf("site limit reached: you can create a maximum of %d sites", user.MaxSites)
		}
	}

	// Validate domain
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if !domainRegex.MatchString(domain) {
		return nil, fmt.Errorf("invalid domain format")
	}

	// Validate site type
	siteType := strings.ToLower(req.SiteType)
	if siteType == "" {
		siteType = "php"
	}
	if siteType != "php" && siteType != "wordpress" {
		return nil, fmt.Errorf("invalid site type: must be 'php' or 'wordpress'")
	}

	phpVersion, err := s.validateRequestedPHPVersion(ctx, req.PHPVersion)
	if err != nil {
		return nil, err
	}

	// Handle slug - auto-generate from domain if not provided
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = strings.ReplaceAll(domain, ".", "_")
		slug = strings.ReplaceAll(slug, "-", "_")
	}

	// Validate slug format
	if !slugRegex.MatchString(slug) {
		return nil, fmt.Errorf("invalid slug format: use letters, numbers, underscores, and hyphens (2-64 chars)")
	}

	// Check if slug already exists for this user
	exists, err := s.db.SlugExists(ctx, req.Username, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to check slug: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("slug '%s' already exists. Choose a different name.", slug)
	}

	// Generate ID and paths
	id := uuid.New().String()
	documentRoot := fmt.Sprintf("/home/%s/apps/%s/public", req.Username, slug)

	// Create site directory via agent
	if err := s.agent.CreateSiteDirectory(ctx, &agent.CreateSiteDirectoryRequest{
		Username: req.Username,
		Domain:   domain,
		Slug:     slug,
		SiteType: siteType,
	}); err != nil {
		return nil, fmt.Errorf("failed to create site directory: %w", err)
	}

	// If WordPress, install it with auto-created database
	if siteType == "wordpress" {
		// Generate database credentials
		// Remove dots and limit to 16 chars for MySQL compatibility
		dbSuffix := strings.ReplaceAll(domain, ".", "")
		if len(dbSuffix) > 16 {
			dbSuffix = dbSuffix[:16]
		}
		dbName := fmt.Sprintf("%s_%s", req.Username, dbSuffix)
		dbUser := dbName
		if len(dbUser) > 32 { // MySQL user name limit
			dbUser = dbUser[:32]
		}
		dbPass := generateDBPassword()

		if err := s.agent.InstallWordPress(ctx, &agent.InstallWordPressRequest{
			Username: req.Username,
			Domain:   domain,
			Path:     documentRoot,
			DBName:   dbName,
			DBUser:   dbUser,
			DBPass:   dbPass,
		}); err != nil {
			return nil, fmt.Errorf("failed to install WordPress: %w", err)
		}

		// Encrypt password for storage
		encryptedPassword, err := crypto.Encrypt(dbPass)
		if err != nil {
			fmt.Printf("warning: failed to encrypt WordPress database password: %v\n", err)
			encryptedPassword = ""
		}

		// Save database record to FastCP database so it appears in the UI
		dbRecord := &database.Database{
			ID:         uuid.New().String(),
			Username:   req.Username,
			DBName:     dbName,
			DBUser:     dbUser,
			DBPassword: encryptedPassword,
		}
		if err := s.db.CreateDatabase(ctx, dbRecord); err != nil {
			// Log but don't fail - WordPress is already installed
			fmt.Printf("warning: failed to save WordPress database record: %v\n", err)
		}
	}

	// Save to database
	dbSite := &database.Site{
		ID:                  id,
		Username:            req.Username,
		Domain:              domain,
		Slug:                slug,
		SiteType:            siteType,
		DocumentRoot:        documentRoot,
		SSLEnabled:          true,
		ForceHTTPS:          false,
		CompressionEnabled:  true,
		GzipEnabled:         true,
		ZstdEnabled:         true,
		CacheControlEnabled: false,
		CacheControlValue:   "",
		PHPVersion:          phpVersion,
		PHPMemoryLimit:      "256M",
		PHPPostMaxSize:      "64M",
		PHPUploadMaxSize:    "64M",
		PHPMaxExecutionTime: 300,
		PHPMaxInputVars:     5000,
	}
	if err := s.db.CreateSite(ctx, dbSite); err != nil {
		return nil, fmt.Errorf("failed to save site: %w", err)
	}

	// Add primary domain to site_domains table
	primaryDomain := &database.SiteDomain{
		SiteID:            id,
		Domain:            domain,
		IsPrimary:         true,
		RedirectToPrimary: false,
	}
	if err := s.db.AddSiteDomain(ctx, primaryDomain); err != nil {
		fmt.Printf("warning: failed to add primary domain: %v\n", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		// Log but don't fail
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return &Site{
		ID:                  id,
		Username:            req.Username,
		Domain:              domain,
		Slug:                slug,
		SiteType:            siteType,
		DocumentRoot:        documentRoot,
		SSLEnabled:          true,
		SSLValid:            false,
		SSLStatus:           "pending",
		SSLReason:           "Certificate not ready yet.",
		ForceHTTPS:          false,
		CompressionEnabled:  true,
		GzipEnabled:         true,
		ZstdEnabled:         true,
		CacheControlEnabled: false,
		CacheControlValue:   "",
		PHPVersion:          phpVersion,
		PHPMemoryLimit:      "256M",
		PHPPostMaxSize:      "64M",
		PHPUploadMaxSize:    "64M",
		PHPMaxExecutionTime: 300,
		PHPMaxInputVars:     5000,
		Domains: []SiteDomain{{
			ID:                primaryDomain.ID,
			SiteID:            id,
			Domain:            domain,
			IsPrimary:         true,
			RedirectToPrimary: false,
			CreatedAt:         primaryDomain.CreatedAt,
		}},
	}, nil
}

// Delete deletes a site
func (s *SiteService) Delete(ctx context.Context, id, username string) error {
	// Get site first to check ownership
	dbSite, err := s.db.GetSite(ctx, id)
	if err != nil {
		return fmt.Errorf("site not found")
	}

	if dbSite.Username != username {
		return fmt.Errorf("site not found")
	}

	// Delete site directory via agent
	if err := s.agent.DeleteSiteDirectory(ctx, &agent.DeleteSiteDirectoryRequest{
		Username: username,
		Slug:     dbSite.Slug,
	}); err != nil {
		return fmt.Errorf("failed to delete site directory: %w", err)
	}

	// Delete from database
	if err := s.db.DeleteSite(ctx, id); err != nil {
		return fmt.Errorf("failed to delete site: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return nil
}

// UpdateSettings updates per-site runtime settings and regenerates Caddy configuration.
func (s *SiteService) UpdateSettings(ctx context.Context, siteID, username string, req *UpdateSiteSettingsRequest) (*Site, error) {
	// Verify ownership first
	dbSite, err := s.db.GetSite(ctx, siteID)
	if err != nil || dbSite.Username != username {
		return nil, fmt.Errorf("site not found")
	}

	cacheControlValue := strings.TrimSpace(req.CacheControlValue)
	cacheControlValue = strings.ReplaceAll(cacheControlValue, "\r", "")
	cacheControlValue = strings.ReplaceAll(cacheControlValue, "\n", "")
	if len(cacheControlValue) > 255 {
		return nil, fmt.Errorf("cache-control value is too long")
	}

	if req.CompressionEnabled && !req.GzipEnabled && !req.ZstdEnabled {
		return nil, fmt.Errorf("enable at least one compression algorithm (gzip or zstd)")
	}
	if req.CacheControlEnabled && cacheControlValue == "" {
		return nil, fmt.Errorf("cache-control value is required when cache-control is enabled")
	}
	requested := req.PHPVersion
	if strings.TrimSpace(requested) == "" {
		requested = dbSite.PHPVersion
	}
	phpVersion, err := s.validateRequestedPHPVersion(ctx, requested)
	if err != nil {
		return nil, err
	}
	phpMemoryLimit, err := normalizePHPSizeSetting(req.PHPMemoryLimit, dbSite.PHPMemoryLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid memory limit: %w", err)
	}
	phpPostMaxSize, err := normalizePHPSizeSetting(req.PHPPostMaxSize, dbSite.PHPPostMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid post max size: %w", err)
	}
	phpUploadMaxSize, err := normalizePHPSizeSetting(req.PHPUploadMaxSize, dbSite.PHPUploadMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid upload max filesize: %w", err)
	}
	postMaxBytes, err := parsePHPSizeBytes(phpPostMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid post max size: %w", err)
	}
	uploadMaxBytes, err := parsePHPSizeBytes(phpUploadMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid upload max filesize: %w", err)
	}
	if postMaxBytes < uploadMaxBytes {
		return nil, fmt.Errorf("post max size must be greater than or equal to upload max filesize")
	}
	phpMaxExecutionTime := req.PHPMaxExecutionTime
	if phpMaxExecutionTime <= 0 {
		phpMaxExecutionTime = dbSite.PHPMaxExecutionTime
	}
	if phpMaxExecutionTime < 1 || phpMaxExecutionTime > 3600 {
		return nil, fmt.Errorf("max execution time must be between 1 and 3600 seconds")
	}
	phpMaxInputVars := req.PHPMaxInputVars
	if phpMaxInputVars <= 0 {
		phpMaxInputVars = dbSite.PHPMaxInputVars
	}
	if phpMaxInputVars < 100 || phpMaxInputVars > 100000 {
		return nil, fmt.Errorf("max input vars must be between 100 and 100000")
	}

	// When compression is disabled, force algorithms off for clarity.
	gzipEnabled := req.GzipEnabled
	zstdEnabled := req.ZstdEnabled
	if !req.CompressionEnabled {
		gzipEnabled = false
		zstdEnabled = false
	}

	if err := s.db.UpdateSiteSettings(
		ctx,
		siteID,
		username,
		req.ForceHTTPS,
		req.CompressionEnabled,
		gzipEnabled,
		zstdEnabled,
		req.CacheControlEnabled,
		cacheControlValue,
		phpVersion,
		phpMemoryLimit,
		phpPostMaxSize,
		phpUploadMaxSize,
		phpMaxExecutionTime,
		phpMaxInputVars,
	); err != nil {
		if err == context.Canceled {
			return nil, err
		}
		return nil, fmt.Errorf("failed to update site settings: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		// Roll back database settings so failed runtime apply doesn't leave site pointed at a broken PHP backend.
		rollbackErr := s.db.UpdateSiteSettings(
			ctx,
			siteID,
			username,
			dbSite.ForceHTTPS,
			dbSite.CompressionEnabled,
			dbSite.GzipEnabled,
			dbSite.ZstdEnabled,
			dbSite.CacheControlEnabled,
			dbSite.CacheControlValue,
			dbSite.PHPVersion,
			dbSite.PHPMemoryLimit,
			dbSite.PHPPostMaxSize,
			dbSite.PHPUploadMaxSize,
			dbSite.PHPMaxExecutionTime,
			dbSite.PHPMaxInputVars,
		)
		if rollbackErr != nil {
			return nil, fmt.Errorf("failed to apply site settings and rollback failed: apply_error=%v rollback_error=%v", err, rollbackErr)
		}
		// Best effort: restore runtime config after rollback.
		if reloadErr := s.agent.ReloadCaddy(ctx); reloadErr != nil {
			return nil, fmt.Errorf("failed to apply site settings, rolled back database values, but failed to restore runtime config: apply_error=%v restore_error=%v", err, reloadErr)
		}
		return nil, fmt.Errorf("failed to apply site settings; changes were rolled back: %w", err)
	}

	return s.Get(ctx, siteID, username)
}

const maxDomainsPerSite = 20

// AddDomain adds a domain to a site
func (s *SiteService) AddDomain(ctx context.Context, req *AddDomainRequest) (*SiteDomain, error) {
	// Validate domain
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if !domainRegex.MatchString(domain) {
		return nil, fmt.Errorf("invalid domain format")
	}

	// Verify site ownership
	dbSite, err := s.db.GetSite(ctx, req.SiteID)
	if err != nil {
		return nil, fmt.Errorf("site not found")
	}
	if dbSite.Username != req.Username {
		return nil, fmt.Errorf("site not found")
	}

	// Check domain limit
	existingDomains, err := s.db.GetSiteDomains(ctx, req.SiteID)
	if err != nil {
		return nil, fmt.Errorf("failed to check domain count: %w", err)
	}
	if len(existingDomains) >= maxDomainsPerSite {
		return nil, fmt.Errorf("maximum of %d domains per site reached", maxDomainsPerSite)
	}

	// Add domain
	dbDomain := &database.SiteDomain{
		SiteID:            req.SiteID,
		Domain:            domain,
		IsPrimary:         false,
		RedirectToPrimary: req.RedirectToPrimary,
	}
	if err := s.db.AddSiteDomain(ctx, dbDomain); err != nil {
		return nil, fmt.Errorf("failed to add domain: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return &SiteDomain{
		ID:                dbDomain.ID,
		SiteID:            dbDomain.SiteID,
		Domain:            dbDomain.Domain,
		IsPrimary:         dbDomain.IsPrimary,
		RedirectToPrimary: dbDomain.RedirectToPrimary,
		CreatedAt:         dbDomain.CreatedAt,
	}, nil
}

// UpdateDomain updates a domain's settings
func (s *SiteService) UpdateDomain(ctx context.Context, req *UpdateDomainRequest) error {
	// Get domain
	dbDomain, err := s.db.GetSiteDomain(ctx, req.DomainID)
	if err != nil {
		return fmt.Errorf("domain not found")
	}

	// Verify site ownership
	dbSite, err := s.db.GetSite(ctx, dbDomain.SiteID)
	if err != nil {
		return fmt.Errorf("site not found")
	}
	if dbSite.Username != req.Username {
		return fmt.Errorf("site not found")
	}

	// Can't set redirect on primary domain
	if dbDomain.IsPrimary && req.RedirectToPrimary {
		return fmt.Errorf("cannot redirect primary domain")
	}

	dbDomain.RedirectToPrimary = req.RedirectToPrimary
	if err := s.db.UpdateSiteDomain(ctx, dbDomain); err != nil {
		return fmt.Errorf("failed to update domain: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return nil
}

// SetPrimaryDomain sets a domain as the primary domain for a site
func (s *SiteService) SetPrimaryDomain(ctx context.Context, req *SetPrimaryDomainRequest) error {
	// Get domain
	dbDomain, err := s.db.GetSiteDomain(ctx, req.DomainID)
	if err != nil {
		return fmt.Errorf("domain not found")
	}

	// Verify site ownership
	dbSite, err := s.db.GetSite(ctx, dbDomain.SiteID)
	if err != nil {
		return fmt.Errorf("site not found")
	}
	if dbSite.Username != req.Username {
		return fmt.Errorf("site not found")
	}

	// Set as primary
	if err := s.db.SetPrimaryDomain(ctx, dbDomain.SiteID, req.DomainID); err != nil {
		return fmt.Errorf("failed to set primary domain: %w", err)
	}

	// Update the site's main domain field
	dbSite.Domain = dbDomain.Domain
	// Note: We would need an UpdateSite method, but for now we just update Caddy

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return nil
}

// DeleteDomain removes a domain from a site
func (s *SiteService) DeleteDomain(ctx context.Context, req *DeleteDomainRequest) error {
	// Get domain
	dbDomain, err := s.db.GetSiteDomain(ctx, req.DomainID)
	if err != nil {
		return fmt.Errorf("domain not found")
	}

	// Verify site ownership
	dbSite, err := s.db.GetSite(ctx, dbDomain.SiteID)
	if err != nil {
		return fmt.Errorf("site not found")
	}
	if dbSite.Username != req.Username {
		return fmt.Errorf("site not found")
	}

	// Can't delete primary domain
	if dbDomain.IsPrimary {
		return fmt.Errorf("cannot delete primary domain")
	}

	if err := s.db.DeleteSiteDomain(ctx, req.DomainID); err != nil {
		return fmt.Errorf("failed to delete domain: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
	}

	return nil
}

func generateDBPassword() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ValidateSlug checks if a slug is valid and available
func (s *SiteService) ValidateSlug(ctx context.Context, username, slug string) (bool, string, error) {
	// Validate format
	if !slugRegex.MatchString(slug) {
		return false, "Invalid format: use letters, numbers, underscores, and hyphens (2-64 chars)", nil
	}

	// Check if already exists
	exists, err := s.db.SlugExists(ctx, username, slug)
	if err != nil {
		return false, "", err
	}
	if exists {
		return false, "This slug is already in use", nil
	}

	return true, "Slug is available", nil
}

// ValidateDomain checks if a domain is valid and available
func (s *SiteService) ValidateDomain(ctx context.Context, domain string) (bool, string, error) {
	// Normalize domain
	domain = strings.ToLower(strings.TrimSpace(domain))

	if domain == "" {
		return false, "Domain is required", nil
	}

	// Validate format
	if !domainRegex.MatchString(domain) {
		return false, "Invalid domain format", nil
	}

	// Check minimum length
	if len(domain) < 3 {
		return false, "Domain is too short", nil
	}

	// Check if already exists
	exists, err := s.db.DomainExists(ctx, domain)
	if err != nil {
		return false, "", err
	}
	if exists {
		return false, "This domain is already in use", nil
	}

	return true, "Domain is available", nil
}

func (s *SiteService) validateRequestedPHPVersion(ctx context.Context, requested string) (string, error) {
	phpVersion := strings.TrimSpace(requested)
	if phpVersion == "" {
		phpVersion = "8.4"
	}
	cfg, err := s.agent.GetPHPDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to validate php version against installed runtimes: %w", err)
	}
	for _, v := range cfg.AvailablePHPVersions {
		if phpVersion == strings.TrimSpace(v) {
			return phpVersion, nil
		}
	}
	if len(cfg.AvailablePHPVersions) == 0 {
		return "", fmt.Errorf("no supported php-fpm versions are installed on this server")
	}
	return "", fmt.Errorf("php version %s is not installed on this server", phpVersion)
}

// GenerateSlug creates a slug from a domain name
func GenerateSlug(domain string) string {
	slug := strings.ReplaceAll(domain, ".", "_")
	slug = strings.ReplaceAll(slug, "-", "_")
	return slug
}
