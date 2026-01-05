package sites

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/jail"
	"github.com/rehmatworks/fastcp/internal/models"
)

var (
	ErrSiteNotFound      = errors.New("site not found")
	ErrDomainExists      = errors.New("domain already exists")
	ErrInvalidDomain     = errors.New("invalid domain name")
	ErrInvalidSiteName   = errors.New("invalid site name")
	ErrInvalidPHPVersion = errors.New("invalid PHP version")
	ErrSiteLimitReached  = errors.New("site limit reached")

	// Domain validation regex - allows letters, numbers, hyphens, dots
	// Must start and end with alphanumeric, no consecutive dots/hyphens
	domainRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	
	// Site name validation - alphanumeric, spaces, hyphens, underscores
	siteNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\s\-_\.]{1,100}$`)
)

// Manager manages website configurations
type Manager struct {
	sites      map[string]*models.Site
	domains    map[string]string                 // domain -> site ID
	userLimits map[string]*models.UserLimits     // username -> limits
	mu         sync.RWMutex
	dataPath   string
}

// NewManager creates a new site manager
func NewManager(dataPath string) *Manager {
	return &Manager{
		sites:      make(map[string]*models.Site),
		domains:    make(map[string]string),
		userLimits: make(map[string]*models.UserLimits),
		dataPath:   dataPath,
	}
}

// Load loads sites and user limits from storage
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load sites
	sitesFile := filepath.Join(m.dataPath, "sites.json")
	data, err := os.ReadFile(sitesFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		var sites []*models.Site
		if err := json.Unmarshal(data, &sites); err != nil {
			return err
		}

		for _, site := range sites {
			m.sites[site.ID] = site
			m.domains[site.Domain] = site.ID
			for _, alias := range site.Aliases {
				m.domains[alias] = site.ID
			}
		}
	}

	// Load user limits
	limitsFile := filepath.Join(m.dataPath, "user_limits.json")
	limitsData, err := os.ReadFile(limitsFile)
	if err == nil {
		var limits []*models.UserLimits
		if err := json.Unmarshal(limitsData, &limits); err == nil {
			for _, limit := range limits {
				m.userLimits[limit.Username] = limit
			}
		}
	}

	return nil
}

// Save saves sites to storage
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sites := make([]*models.Site, 0, len(m.sites))
	for _, site := range m.sites {
		sites = append(sites, site)
	}

	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(m.dataPath, "sites.json"), data, 0644)
}

// Create creates a new site
func (m *Manager) Create(site *models.Site) (*models.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate domain name
	if !isValidDomain(site.Domain) {
		return nil, ErrInvalidDomain
	}
	for _, alias := range site.Aliases {
		if !isValidDomain(alias) {
			return nil, fmt.Errorf("invalid alias: %s", alias)
		}
	}

	// Validate site name (prevent XSS)
	if site.Name != "" && !isValidSiteName(site.Name) {
		return nil, ErrInvalidSiteName
	}

	// Check if domain already exists
	if _, exists := m.domains[site.Domain]; exists {
		return nil, ErrDomainExists
	}
	for _, alias := range site.Aliases {
		if _, exists := m.domains[alias]; exists {
			return nil, fmt.Errorf("alias %s already exists", alias)
		}
	}

	// Check site limit for user
	if site.UserID != "" && site.UserID != "admin" {
		username := getUsernameFromID(site.UserID)
		if limit, ok := m.userLimits[username]; ok && limit.MaxSites > 0 {
			currentCount := m.countUserSitesUnlocked(site.UserID)
			if currentCount >= limit.MaxSites {
				return nil, ErrSiteLimitReached
			}
		}
	}

	// Validate PHP version
	cfg := config.Get()
	validPHP := false
	for _, pv := range cfg.PHPVersions {
		if pv.Version == site.PHPVersion && pv.Enabled {
			validPHP = true
			break
		}
	}
	if !validPHP {
		return nil, ErrInvalidPHPVersion
	}

	// Set defaults
	if site.ID == "" {
		site.ID = uuid.New().String()
	}
	if site.Status == "" {
		site.Status = "active"
	}
	if site.PublicPath == "" {
		site.PublicPath = "public"
	}
	
	// Set root path based on user ownership
	// Structure: /var/www/{username}/{domain}/
	if site.RootPath == "" {
		username := getUsernameFromID(site.UserID)
		if username != "" && username != "admin" {
			// User-specific directory
			site.RootPath = filepath.Join(cfg.SitesDir, username, site.Domain)
		} else {
			// Fallback for admin or config-based auth
			site.RootPath = filepath.Join(cfg.SitesDir, site.Domain)
		}
	}

	site.CreatedAt = time.Now()
	site.UpdatedAt = time.Now()

	// Create directory structure
	if err := m.createSiteDirectories(site); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	// Add to maps
	m.sites[site.ID] = site
	m.domains[site.Domain] = site.ID
	for _, alias := range site.Aliases {
		m.domains[alias] = site.ID
	}

	// Save to disk
	if err := m.saveUnlocked(); err != nil {
		return nil, err
	}

	return site, nil
}

// Get retrieves a site by ID
func (m *Manager) Get(id string) (*models.Site, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	site, ok := m.sites[id]
	if !ok {
		return nil, ErrSiteNotFound
	}

	return site, nil
}

// GetByDomain retrieves a site by domain
func (m *Manager) GetByDomain(domain string) (*models.Site, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.domains[domain]
	if !ok {
		return nil, ErrSiteNotFound
	}

	return m.sites[id], nil
}

// List returns all sites, optionally filtered by user
func (m *Manager) List(userID string) []*models.Site {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sites := make([]*models.Site, 0, len(m.sites))
	for _, site := range m.sites {
		if userID == "" || site.UserID == userID {
			sites = append(sites, site)
		}
	}

	return sites
}

// GetAll returns all sites as a slice
func (m *Manager) GetAll() []models.Site {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sites := make([]models.Site, 0, len(m.sites))
	for _, site := range m.sites {
		sites = append(sites, *site)
	}

	return sites
}

// Update updates an existing site
func (m *Manager) Update(id string, updates *models.Site) (*models.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	site, ok := m.sites[id]
	if !ok {
		return nil, ErrSiteNotFound
	}

	// Check if new domain conflicts with existing
	if updates.Domain != "" && updates.Domain != site.Domain {
		if _, exists := m.domains[updates.Domain]; exists {
			return nil, ErrDomainExists
		}
		// Remove old domain mapping
		delete(m.domains, site.Domain)
		site.Domain = updates.Domain
		m.domains[updates.Domain] = id
	}

	// Update fields
	if updates.Name != "" {
		site.Name = updates.Name
	}
	if updates.PHPVersion != "" {
		// Validate PHP version
		cfg := config.Get()
		validPHP := false
		for _, pv := range cfg.PHPVersions {
			if pv.Version == updates.PHPVersion && pv.Enabled {
				validPHP = true
				break
			}
		}
		if !validPHP {
			return nil, ErrInvalidPHPVersion
		}
		site.PHPVersion = updates.PHPVersion
	}
	if updates.Aliases != nil {
		// Remove old alias mappings
		for _, alias := range site.Aliases {
			delete(m.domains, alias)
		}
		// Add new alias mappings
		for _, alias := range updates.Aliases {
			if existingID, exists := m.domains[alias]; exists && existingID != id {
				return nil, fmt.Errorf("alias %s already exists", alias)
			}
			m.domains[alias] = id
		}
		site.Aliases = updates.Aliases
	}
	if updates.PublicPath != "" {
		site.PublicPath = updates.PublicPath
	}
	if updates.Status != "" {
		site.Status = updates.Status
	}

	// Worker mode settings
	site.WorkerMode = updates.WorkerMode
	if updates.WorkerFile != "" {
		site.WorkerFile = updates.WorkerFile
	}
	if updates.WorkerNum > 0 {
		site.WorkerNum = updates.WorkerNum
	}

	if updates.Environment != nil {
		site.Environment = updates.Environment
	}

	site.UpdatedAt = time.Now()

	// Save to disk
	if err := m.saveUnlocked(); err != nil {
		return nil, err
	}

	return site, nil
}

// Delete removes a site
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	site, ok := m.sites[id]
	if !ok {
		return ErrSiteNotFound
	}

	// Remove site directory from filesystem
	if site.RootPath != "" && site.RootPath != "/" && site.RootPath != "/var/www" {
		if err := os.RemoveAll(site.RootPath); err != nil {
			// Log but don't fail - directory might already be gone
			slog.Warn("failed to remove site directory", "path", site.RootPath, "error", err)
		} else {
			slog.Info("removed site directory", "path", site.RootPath)
		}
	}

	// Remove domain mappings
	delete(m.domains, site.Domain)
	for _, alias := range site.Aliases {
		delete(m.domains, alias)
	}

	// Remove site
	delete(m.sites, id)

	// Save to disk
	return m.saveUnlocked()
}

// Suspend suspends a site
func (m *Manager) Suspend(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	site, ok := m.sites[id]
	if !ok {
		return ErrSiteNotFound
	}

	site.Status = "suspended"
	site.UpdatedAt = time.Now()

	return m.saveUnlocked()
}

// Unsuspend reactivates a suspended site
func (m *Manager) Unsuspend(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	site, ok := m.sites[id]
	if !ok {
		return ErrSiteNotFound
	}

	site.Status = "active"
	site.UpdatedAt = time.Now()

	return m.saveUnlocked()
}

// createSiteDirectories creates the directory structure for a site with proper ownership
func (m *Manager) createSiteDirectories(site *models.Site) error {
	cfg := config.Get()

	// Get owner info
	username := getUsernameFromID(site.UserID)
	uid, gid := getUIDGID(site.UserID)

	// For jailed users, the www directory is inside their home
	// For non-jailed users, it's in /var/www/username
	var userBaseDir string
	if username != "" && username != "admin" && runtime.GOOS == "linux" {
		isJailed := jail.IsUserJailed(username)
		if isJailed {
			// Jailed user - www is in home directory
			userBaseDir = filepath.Join("/home", username, "www")
			// For jailed users, the RootPath should be updated
			site.RootPath = filepath.Join(userBaseDir, site.Domain)
		} else {
			// Non-jailed user - www is in /var/www
			userBaseDir = filepath.Join(cfg.SitesDir, username)
		}
		
		if err := os.MkdirAll(userBaseDir, 0755); err != nil {
			return err
		}
		// Set ownership on user base directory
		setOwnership(userBaseDir, uid, gid)
		// Set ACL to prevent other users from accessing
		setACL(userBaseDir, username)
	}

	dirs := []string{
		site.RootPath,
		filepath.Join(site.RootPath, site.PublicPath),
		filepath.Join(cfg.LogDir, "sites", site.ID),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		// Set ownership for site directories
		if runtime.GOOS == "linux" && uid > 0 {
			setOwnership(dir, uid, gid)
		}
	}

	// Create default index.php with beautiful FastCP landing page
	indexPath := filepath.Join(site.RootPath, site.PublicPath, "index.php")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		// Escape site name for safe PHP string embedding
		escapedSiteName := strings.ReplaceAll(site.Name, `\`, `\\`)
		escapedSiteName = strings.ReplaceAll(escapedSiteName, `'`, `\'`)
		escapedDomain := strings.ReplaceAll(site.Domain, `\`, `\\`)
		escapedDomain = strings.ReplaceAll(escapedDomain, `'`, `\'`)

		content := fmt.Sprintf(`<?php
// Site: %s
// Domain: %s
// Powered by FastCP - https://fastcp.org
$siteName = '%s';
$siteDomain = '%s';
?>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title><?= htmlspecialchars($siteName) ?> - Powered by FastCP</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', system-ui, sans-serif;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            background: linear-gradient(135deg, #0f172a 0%%, #1e293b 50%%, #0f172a 100%%);
            color: #f8fafc;
            padding: 1.5rem;
        }
        .container {
            text-align: center;
            max-width: 480px;
        }
        .logo {
            width: 80px;
            height: 80px;
            margin: 0 auto 1.5rem;
            background: linear-gradient(135deg, #10b981, #059669);
            border-radius: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
            box-shadow: 0 20px 50px rgba(16, 185, 129, 0.3);
        }
        .logo span {
            font-size: 2.5rem;
            font-weight: 700;
            color: white;
        }
        h1 {
            font-size: 2rem;
            font-weight: 700;
            margin-bottom: 0.5rem;
            background: linear-gradient(135deg, #f8fafc, #94a3b8);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }
        .domain {
            color: #10b981;
            font-size: 1rem;
            font-weight: 500;
            margin-bottom: 2rem;
        }
        .card {
            background: rgba(255,255,255,0.03);
            border: 1px solid rgba(255,255,255,0.06);
            border-radius: 16px;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            backdrop-filter: blur(10px);
        }
        .card p {
            color: #94a3b8;
            line-height: 1.7;
            font-size: 0.95rem;
        }
        .stats {
            display: flex;
            justify-content: center;
            gap: 2rem;
            margin-top: 1rem;
            padding-top: 1rem;
            border-top: 1px solid rgba(255,255,255,0.06);
        }
        .stat {
            text-align: center;
        }
        .stat-value {
            font-size: 1.1rem;
            font-weight: 600;
            color: #f8fafc;
        }
        .stat-label {
            font-size: 0.75rem;
            color: #64748b;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-top: 0.25rem;
        }
        .footer {
            margin-top: 2rem;
            color: #475569;
            font-size: 0.8rem;
        }
        .footer a {
            color: #10b981;
            text-decoration: none;
        }
        .footer a:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo"><span>F</span></div>
        <h1><?= htmlspecialchars($siteName) ?></h1>
        <p class="domain"><?= htmlspecialchars($_SERVER['HTTP_HOST']) ?></p>
        
        <div class="card">
            <p>Your site is live and ready! Replace this page with your application by uploading files to the public directory.</p>
            <div class="stats">
                <div class="stat">
                    <div class="stat-value"><?= PHP_VERSION ?></div>
                    <div class="stat-label">PHP Version</div>
                </div>
                <div class="stat">
                    <div class="stat-value"><?= php_uname('s') ?></div>
                    <div class="stat-label">Platform</div>
                </div>
            </div>
        </div>
        
        <p class="footer">
            Managed by <a href="https://fastcp.org" target="_blank">FastCP</a> • 
            High-Performance PHP Hosting
        </p>
    </div>
</body>
</html>
`, site.Name, site.Domain, escapedSiteName, escapedDomain)
		if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
			return err
		}
		// Set ownership on the index file
		if runtime.GOOS == "linux" {
			uid, gid := getUIDGID(site.UserID)
			if uid > 0 {
				_ = os.Chown(indexPath, uid, gid)
			}
		}
	}

	// Set ownership recursively on the site root
	if runtime.GOOS == "linux" {
		uid, gid := getUIDGID(site.UserID)
		if uid > 0 {
			_ = setOwnershipRecursive(site.RootPath, uid, gid)
		}
	}

	return nil
}

// saveUnlocked saves sites without acquiring lock (caller must hold lock)
func (m *Manager) saveUnlocked() error {
	sites := make([]*models.Site, 0, len(m.sites))
	for _, site := range m.sites {
		sites = append(sites, site)
	}

	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(m.dataPath, "sites.json"), data, 0644)
}

// CountByPHPVersion returns the count of sites using each PHP version
func (m *Manager) CountByPHPVersion() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for _, site := range m.sites {
		if site.Status == "active" {
			counts[site.PHPVersion]++
		}
	}

	return counts
}

// GetStats returns site statistics
func (m *Manager) GetStats() (total, active int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total = len(m.sites)
	for _, site := range m.sites {
		if site.Status == "active" {
			active++
		}
	}

	return
}

// getUsernameFromID looks up the Unix username from a user ID
func getUsernameFromID(userID string) string {
	if userID == "" || userID == "admin" {
		return ""
	}

	// Try to look up user by UID
	u, err := user.LookupId(userID)
	if err != nil {
		// Maybe it's a username, not a UID
		u, err = user.Lookup(userID)
		if err != nil {
			return ""
		}
	}
	return u.Username
}

// getUIDGID returns the numeric UID and GID for a user
func getUIDGID(userID string) (int, int) {
	if userID == "" || userID == "admin" {
		return 0, 0
	}

	u, err := user.LookupId(userID)
	if err != nil {
		u, err = user.Lookup(userID)
		if err != nil {
			return 0, 0
		}
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid
}

// setOwnership sets the owner and group of a path
func setOwnership(path string, uid, gid int) error {
	if uid <= 0 {
		return nil
	}
	return os.Chown(path, uid, gid)
}

// setOwnershipRecursive sets ownership recursively on a directory
func setOwnershipRecursive(path string, uid, gid int) error {
	if uid <= 0 {
		return nil
	}
	
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, uid, gid)
	})
}

// setACL sets POSIX ACLs to restrict access to a directory
// Also grants read access to the fastcp user for PHP execution
func setACL(path, username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// First, set strict Unix permissions recursively (no access for others)
	_ = exec.Command("chmod", "-R", "750", path).Run()

	// Remove all default ACLs and set strict permissions recursively
	// Owner has full access, fastcp user has full access for PHP/WordPress, others have none
	cmds := [][]string{
		// Remove existing ACLs recursively
		{"setfacl", "-R", "-b", path},
		// Set owner access recursively
		{"setfacl", "-R", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		// Set fastcp (PHP) user full access recursively (needed for WordPress plugin/theme management)
		{"setfacl", "-R", "-m", "u:fastcp:rwx", path},
		// Set root access recursively
		{"setfacl", "-R", "-m", "u:root:rwx", path},
		// Remove group access recursively
		{"setfacl", "-R", "-m", "g::---", path},
		// Remove other users' access recursively
		{"setfacl", "-R", "-m", "o::---", path},
		// Set default ACL for new files/dirs (inherit) - only on directories
		{"setfacl", "-d", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		{"setfacl", "-d", "-m", "u:fastcp:rwx", path},
		{"setfacl", "-d", "-m", "u:root:rwx", path},
		{"setfacl", "-d", "-m", "g::---", path},
		{"setfacl", "-d", "-m", "o::---", path},
	}

	for _, cmdArgs := range cmds {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		// Ignore errors - setfacl might not be installed
		_ = cmd.Run()
	}

	return nil
}

// SecureBaseDirectory ensures /var/www has proper permissions
func SecureBaseDirectory(sitesDir string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// /var/www should be owned by root with 751 permissions
	// This allows traversal but not listing
	_ = exec.Command("chown", "root:root", sitesDir).Run()
	_ = exec.Command("chmod", "751", sitesDir).Run()

	return nil
}

// countUserSitesUnlocked counts sites for a user (caller must hold lock)
func (m *Manager) countUserSitesUnlocked(userID string) int {
	count := 0
	for _, site := range m.sites {
		if site.UserID == userID {
			count++
		}
	}
	return count
}

// GetUserLimit returns the site limit for a user
func (m *Manager) GetUserLimit(username string) *models.UserLimits {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit, ok := m.userLimits[username]; ok {
		return limit
	}
	return &models.UserLimits{Username: username, MaxSites: 0}
}

// SetUserLimit sets the site limit for a user
func (m *Manager) SetUserLimit(limit *models.UserLimits) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userLimits[limit.Username] = limit
	return m.saveUserLimitsUnlocked()
}

// saveUserLimitsUnlocked saves user limits (caller must hold lock)
func (m *Manager) saveUserLimitsUnlocked() error {
	limits := make([]*models.UserLimits, 0, len(m.userLimits))
	for _, limit := range m.userLimits {
		limits = append(limits, limit)
	}

	data, err := json.MarshalIndent(limits, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(m.dataPath, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(m.dataPath, "user_limits.json"), data, 0644)
}

// DatabaseManager interface for WordPress installation
type DatabaseManager interface {
	Create(db *models.Database) (*models.Database, error)
	EnsureMySQL() error
}

// InstallWordPress downloads and configures WordPress for a site
func (m *Manager) InstallWordPress(site *models.Site, dbManager DatabaseManager) (*models.Database, error) {
	publicPath := filepath.Join(site.RootPath, site.PublicPath)

	// Ensure MySQL is available
	if err := dbManager.EnsureMySQL(); err != nil {
		return nil, fmt.Errorf("MySQL not available: %w", err)
	}

	// Generate database name from domain (sanitized)
	dbName := sanitizeForMySQL(site.Domain)
	if len(dbName) > 16 {
		dbName = dbName[:16]
	}
	dbName = "wp_" + dbName

	// Create database
	db := &models.Database{
		UserID:   site.UserID,
		SiteID:   site.ID,
		Name:     dbName,
		Username: dbName,
		Host:     "localhost",
		Port:     3306,
	}

	createdDB, err := dbManager.Create(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Download WordPress
	wpZipPath := filepath.Join(os.TempDir(), fmt.Sprintf("wordpress-%s.zip", site.ID))
	defer os.Remove(wpZipPath)

	fmt.Println("[WordPress] Downloading WordPress...")
	if err := downloadFile("https://wordpress.org/latest.zip", wpZipPath); err != nil {
		return nil, fmt.Errorf("failed to download WordPress: %w", err)
	}

	// Remove existing files in public directory (except hidden files)
	entries, _ := os.ReadDir(publicPath)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			os.RemoveAll(filepath.Join(publicPath, entry.Name()))
		}
	}

	// Extract WordPress
	fmt.Println("[WordPress] Extracting WordPress...")
	if err := unzipWordPress(wpZipPath, publicPath); err != nil {
		return nil, fmt.Errorf("failed to extract WordPress: %w", err)
	}

	// Create wp-config.php
	fmt.Println("[WordPress] Configuring wp-config.php...")
	if err := createWPConfig(publicPath, createdDB); err != nil {
		return nil, fmt.Errorf("failed to create wp-config.php: %w", err)
	}

	// Set correct ownership
	if runtime.GOOS == "linux" {
		uid, gid := getUIDGID(site.UserID)
		if uid > 0 {
			setOwnershipRecursive(publicPath, uid, gid)
		}
	}

	fmt.Println("[WordPress] Installation complete!")
	return createdDB, nil
}

// sanitizeForMySQL removes invalid characters for MySQL identifiers
func sanitizeForMySQL(s string) string {
	result := ""
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result += string(c)
		} else if c == '.' || c == '-' {
			result += "_"
		}
	}
	return result
}

// downloadFile downloads a file from URL to local path
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// unzipWordPress extracts WordPress ZIP, moving contents from wordpress/ subfolder
func unzipWordPress(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// WordPress ZIP has everything under "wordpress/" folder
		// We want to extract contents directly to destPath
		name := f.Name
		if strings.HasPrefix(name, "wordpress/") {
			name = strings.TrimPrefix(name, "wordpress/")
		}
		if name == "" {
			continue
		}

		fpath := filepath.Join(destPath, name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(destPath)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// createWPConfig creates wp-config.php with database credentials
func createWPConfig(publicPath string, db *models.Database) error {
	// Generate unique salts
	salts := generateWPSalts()

	// Use 127.0.0.1 instead of localhost to avoid socket connection issues
	dbHost := db.Host
	if dbHost == "localhost" {
		dbHost = "127.0.0.1"
	}

	config := fmt.Sprintf(`<?php
/**
 * WordPress Configuration - Generated by FastCP
 * https://fastcp.org
 */

// ** Database settings ** //
define( 'DB_NAME', '%s' );
define( 'DB_USER', '%s' );
define( 'DB_PASSWORD', '%s' );
define( 'DB_HOST', '%s' );
define( 'DB_CHARSET', 'utf8mb4' );
define( 'DB_COLLATE', '' );

/**
 * Authentication unique keys and salts.
 */
%s

/**
 * WordPress database table prefix.
 */
$table_prefix = 'wp_';

/**
 * WordPress debugging mode.
 */
define( 'WP_DEBUG', false );

/* Add any custom values between this line and the "stop editing" line. */

// Enable automatic updates
define( 'WP_AUTO_UPDATE_CORE', 'minor' );

// Increase memory limit
define( 'WP_MEMORY_LIMIT', '256M' );

/**
 * HTTPS/SSL Configuration
 * Automatically detect and use the correct protocol
 */
if ( isset( $_SERVER['HTTPS'] ) && $_SERVER['HTTPS'] === 'on' ) {
    $_SERVER['REQUEST_SCHEME'] = 'https';
} elseif ( ! empty( $_SERVER['HTTP_X_FORWARDED_PROTO'] ) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https' ) {
    $_SERVER['HTTPS'] = 'on';
    $_SERVER['REQUEST_SCHEME'] = 'https';
}

// Force HTTPS for admin and login
define( 'FORCE_SSL_ADMIN', true );

// Set site URL dynamically based on request
if ( ! defined( 'WP_HOME' ) ) {
    $protocol = ( isset( $_SERVER['HTTPS'] ) && $_SERVER['HTTPS'] === 'on' ) ? 'https://' : 'http://';
    define( 'WP_HOME', $protocol . $_SERVER['HTTP_HOST'] );
    define( 'WP_SITEURL', $protocol . $_SERVER['HTTP_HOST'] );
}

/* That's all, stop editing! Happy publishing. */

/** Absolute path to the WordPress directory. */
if ( ! defined( 'ABSPATH' ) ) {
	define( 'ABSPATH', __DIR__ . '/' );
}

/** Sets up WordPress vars and included files. */
require_once ABSPATH . 'wp-settings.php';
`, db.Name, db.Username, db.Password, dbHost, salts)

	return os.WriteFile(filepath.Join(publicPath, "wp-config.php"), []byte(config), 0640)
}

// generateWPSalts generates WordPress security salts
func generateWPSalts() string {
	keys := []string{
		"AUTH_KEY",
		"SECURE_AUTH_KEY",
		"LOGGED_IN_KEY",
		"NONCE_KEY",
		"AUTH_SALT",
		"SECURE_AUTH_SALT",
		"LOGGED_IN_SALT",
		"NONCE_SALT",
	}

	var salts []string
	for _, key := range keys {
		salt := generateRandomString(64)
		salts = append(salts, fmt.Sprintf("define( '%s', '%s' );", key, salt))
	}

	return strings.Join(salts, "\n")
}

// generateRandomString generates a random string for salts
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

// isValidDomain validates a domain name
func isValidDomain(domain string) bool {
	if domain == "" || len(domain) > 253 {
		return false
	}
	
	// Allow localhost for development
	if domain == "localhost" {
		return true
	}
	
	// Check for path traversal attempts
	if strings.Contains(domain, "..") || strings.Contains(domain, "/") || strings.Contains(domain, "\\") {
		return false
	}
	
	// Check for shell metacharacters
	dangerousChars := []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "[", "]", "<", ">", "!", "~", "*", "?", "'", "\"", "\n", "\r", "\t"}
	for _, char := range dangerousChars {
		if strings.Contains(domain, char) {
			return false
		}
	}
	
	// Validate domain format
	return domainRegex.MatchString(domain) || strings.HasSuffix(domain, ".test") || strings.HasSuffix(domain, ".local")
}

// isValidSiteName validates a site name
func isValidSiteName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	
	// Check for dangerous characters that could cause XSS or injection
	dangerousChars := []string{"<", ">", "\"", "'", "&", ";", "|", "$", "`", "\\", "\n", "\r"}
	for _, char := range dangerousChars {
		if strings.Contains(name, char) {
			return false
		}
	}
	
	return siteNameRegex.MatchString(name)
}

