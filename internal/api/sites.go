package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
var slugRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

// SiteService handles site operations
type SiteService struct {
	db    *database.DB
	agent *agent.Client
}

// NewSiteService creates a new site service
func NewSiteService(db *database.DB, agent *agent.Client) *SiteService {
	return &SiteService{db: db, agent: agent}
}

// List returns all sites for a user
func (s *SiteService) List(ctx context.Context, username string) ([]*Site, error) {
	dbSites, err := s.db.ListSites(ctx, username)
	if err != nil {
		return nil, err
	}

	sites := make([]*Site, len(dbSites))
	for i, dbSite := range dbSites {
		// Get domains for this site
		domains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)
		sites[i] = &Site{
			ID:                  dbSite.ID,
			Username:            dbSite.Username,
			Domain:              dbSite.Domain,
			Slug:                dbSite.Slug,
			SiteType:            dbSite.SiteType,
			DocumentRoot:        dbSite.DocumentRoot,
			SSLEnabled:          dbSite.SSLEnabled,
			CompressionEnabled:  dbSite.CompressionEnabled,
			GzipEnabled:         dbSite.GzipEnabled,
			ZstdEnabled:         dbSite.ZstdEnabled,
			CacheControlEnabled: dbSite.CacheControlEnabled,
			CacheControlValue:   dbSite.CacheControlValue,
			CreatedAt:           dbSite.CreatedAt,
			Domains:             convertDomains(domains),
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
	for i, dbSite := range dbSites {
		domains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)
		sites[i] = &Site{
			ID:                  dbSite.ID,
			Username:            dbSite.Username,
			Domain:              dbSite.Domain,
			Slug:                dbSite.Slug,
			SiteType:            dbSite.SiteType,
			DocumentRoot:        dbSite.DocumentRoot,
			SSLEnabled:          dbSite.SSLEnabled,
			CompressionEnabled:  dbSite.CompressionEnabled,
			GzipEnabled:         dbSite.GzipEnabled,
			ZstdEnabled:         dbSite.ZstdEnabled,
			CacheControlEnabled: dbSite.CacheControlEnabled,
			CacheControlValue:   dbSite.CacheControlValue,
			CreatedAt:           dbSite.CreatedAt,
			Domains:             convertDomains(domains),
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
	domains, _ := s.db.GetSiteDomains(ctx, dbSite.ID)

	return &Site{
		ID:                  dbSite.ID,
		Username:            dbSite.Username,
		Domain:              dbSite.Domain,
		Slug:                dbSite.Slug,
		SiteType:            dbSite.SiteType,
		DocumentRoot:        dbSite.DocumentRoot,
		SSLEnabled:          dbSite.SSLEnabled,
		CompressionEnabled:  dbSite.CompressionEnabled,
		GzipEnabled:         dbSite.GzipEnabled,
		ZstdEnabled:         dbSite.ZstdEnabled,
		CacheControlEnabled: dbSite.CacheControlEnabled,
		CacheControlValue:   dbSite.CacheControlValue,
		CreatedAt:           dbSite.CreatedAt,
		Domains:             convertDomains(domains),
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
		CompressionEnabled:  true,
		GzipEnabled:         true,
		ZstdEnabled:         true,
		CacheControlEnabled: false,
		CacheControlValue:   "",
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
		CompressionEnabled:  true,
		GzipEnabled:         true,
		ZstdEnabled:         true,
		CacheControlEnabled: false,
		CacheControlValue:   "",
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

	// When compression is disabled, force algorithms off for clarity.
	gzipEnabled := req.GzipEnabled
	zstdEnabled := req.ZstdEnabled
	if !req.CompressionEnabled {
		gzipEnabled = false
		zstdEnabled = false
	}

	if err := s.db.UpdateSiteSettings(ctx, siteID, username, req.CompressionEnabled, gzipEnabled, zstdEnabled, req.CacheControlEnabled, cacheControlValue); err != nil {
		if err == context.Canceled {
			return nil, err
		}
		return nil, fmt.Errorf("failed to update site settings: %w", err)
	}

	// Reload Caddy configuration
	if err := s.agent.ReloadCaddy(ctx); err != nil {
		fmt.Printf("warning: failed to reload Caddy: %v\n", err)
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

// GenerateSlug creates a slug from a domain name
func GenerateSlug(domain string) string {
	slug := strings.ReplaceAll(domain, ".", "_")
	slug = strings.ReplaceAll(slug, "-", "_")
	return slug
}
