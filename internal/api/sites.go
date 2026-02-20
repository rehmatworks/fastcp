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
	"github.com/rehmatworks/fastcp/internal/database"
)

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

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
			ID:           dbSite.ID,
			Username:     dbSite.Username,
			Domain:       dbSite.Domain,
			SiteType:     dbSite.SiteType,
			DocumentRoot: dbSite.DocumentRoot,
			SSLEnabled:   dbSite.SSLEnabled,
			CreatedAt:    dbSite.CreatedAt,
			Domains:      convertDomains(domains),
		}
	}
	return sites, nil
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
		ID:           dbSite.ID,
		Username:     dbSite.Username,
		Domain:       dbSite.Domain,
		SiteType:     dbSite.SiteType,
		DocumentRoot: dbSite.DocumentRoot,
		SSLEnabled:   dbSite.SSLEnabled,
		CreatedAt:    dbSite.CreatedAt,
		Domains:      convertDomains(domains),
	}, nil
}

// Create creates a new site
func (s *SiteService) Create(ctx context.Context, req *CreateSiteRequest) (*Site, error) {
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

	// Generate ID and paths
	id := uuid.New().String()
	safeDomain := strings.ReplaceAll(domain, ".", "_")
	documentRoot := fmt.Sprintf("/home/%s/apps/%s/public", req.Username, safeDomain)

	// Create site directory via agent
	if err := s.agent.CreateSiteDirectory(ctx, &agent.CreateSiteDirectoryRequest{
		Username: req.Username,
		Domain:   domain,
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

		// Save database record to FastCP database so it appears in the UI
		dbRecord := &database.Database{
			ID:       uuid.New().String(),
			Username: req.Username,
			DBName:   dbName,
			DBUser:   dbUser,
		}
		if err := s.db.CreateDatabase(ctx, dbRecord); err != nil {
			// Log but don't fail - WordPress is already installed
			fmt.Printf("warning: failed to save WordPress database record: %v\n", err)
		}
	}

	// Save to database
	dbSite := &database.Site{
		ID:           id,
		Username:     req.Username,
		Domain:       domain,
		SiteType:     siteType,
		DocumentRoot: documentRoot,
		SSLEnabled:   true,
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
		ID:           id,
		Username:     req.Username,
		Domain:       domain,
		SiteType:     siteType,
		DocumentRoot: documentRoot,
		SSLEnabled:   true,
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
		Domain:   dbSite.Domain,
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
