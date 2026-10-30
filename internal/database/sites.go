package database

import (
	"context"
	"database/sql"
	"time"
)

// Site is a website owned by a user.
type Site struct {
	ID                  string    `json:"id"`
	Username            string    `json:"username"`
	Domain              string    `json:"domain"` // primary domain
	Slug                string    `json:"slug"`
	SiteType            string    `json:"site_type"` // php | wordpress | proxy
	ProxyURL            string    `json:"proxy_url"` // the application a proxy site forwards to
	DocumentRoot        string    `json:"document_root"`
	ForceHTTPS          bool      `json:"force_https"`
	ForceHTTPSAuto      bool      `json:"-"`
	CompressionEnabled  bool      `json:"compression_enabled"`
	GzipEnabled         bool      `json:"gzip_enabled"`
	ZstdEnabled         bool      `json:"zstd_enabled"`
	CacheControlEnabled bool      `json:"cache_control_enabled"`
	CacheControlValue   string    `json:"cache_control_value"`
	PageCache           bool      `json:"page_cache"` // the web server sends WordPress cache plugins' saved pages
	PHPVersion          string    `json:"php_version"`
	PHPMemoryLimit      string    `json:"php_memory_limit"`
	PHPPostMaxSize      string    `json:"php_post_max_size"`
	PHPUploadMaxSize    string    `json:"php_upload_max_filesize"`
	PHPMaxExecutionTime int       `json:"php_max_execution_time"`
	PHPMaxInputVars     int       `json:"php_max_input_vars"`
	CreatedAt           time.Time `json:"created_at"`
}

// SiteDomain is a host name served by a site.
type SiteDomain struct {
	ID                int64  `json:"id"`
	SiteID            string `json:"site_id"`
	Domain            string `json:"domain"`
	IsPrimary         bool   `json:"is_primary"`
	RedirectToPrimary bool   `json:"redirect_to_primary"`
}

const siteColumns = `id, username, domain, COALESCE(slug, ''), COALESCE(site_type, 'php'), proxy_url, document_root,
	COALESCE(force_https, 1), COALESCE(force_https_auto, 0),
	COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
	COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''), page_cache,
	COALESCE(php_version, ''), COALESCE(php_memory_limit, '256M'), COALESCE(php_post_max_size, '64M'),
	COALESCE(php_upload_max_filesize, '64M'), COALESCE(php_max_execution_time, 300), COALESCE(php_max_input_vars, 5000),
	created_at`

func scanSite(sc scanner) (*Site, error) {
	var s Site
	var created sql.NullTime
	err := sc.Scan(&s.ID, &s.Username, &s.Domain, &s.Slug, &s.SiteType, &s.ProxyURL, &s.DocumentRoot,
		&s.ForceHTTPS, &s.ForceHTTPSAuto,
		&s.CompressionEnabled, &s.GzipEnabled, &s.ZstdEnabled,
		&s.CacheControlEnabled, &s.CacheControlValue, &s.PageCache,
		&s.PHPVersion, &s.PHPMemoryLimit, &s.PHPPostMaxSize,
		&s.PHPUploadMaxSize, &s.PHPMaxExecutionTime, &s.PHPMaxInputVars,
		&created)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = created.Time.UTC()
	return &s, nil
}

func (db *DB) querySites(ctx context.Context, where string, args ...any) ([]*Site, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+siteColumns+" FROM sites "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Site
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (db *DB) GetSite(ctx context.Context, id string) (*Site, error) {
	s, err := scanSite(db.QueryRowContext(ctx, "SELECT "+siteColumns+" FROM sites WHERE id = ?", id))
	return s, notFound(err)
}

func (db *DB) ListSites(ctx context.Context, username string) ([]*Site, error) {
	return db.querySites(ctx, "WHERE username = ? ORDER BY domain", username)
}

func (db *DB) ListAllSites(ctx context.Context) ([]*Site, error) {
	return db.querySites(ctx, "ORDER BY username, domain")
}

func (db *DB) CountSites(ctx context.Context, username string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites WHERE username = ?", username).Scan(&n)
	return n, err
}

// CreateSite inserts a site and its primary domain. It returns ErrConflict
// when the domain or slug is taken.
func (db *DB) CreateSite(ctx context.Context, s *Site) error {
	s.CreatedAt = time.Now().UTC()
	return db.InTx(ctx, func(tx *sql.Tx) error {
		if taken, err := domainTaken(ctx, tx, s.Domain); err != nil {
			return err
		} else if taken {
			return ErrConflict
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sites (id, username, domain, slug, site_type, proxy_url, document_root, ssl_enabled, force_https, force_https_auto,
				compression_enabled, gzip_enabled, zstd_enabled, cache_control_enabled, cache_control_value, page_cache,
				php_version, php_memory_limit, php_post_max_size, php_upload_max_filesize, php_max_execution_time, php_max_input_vars,
				created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Username, s.Domain, s.Slug, s.SiteType, s.ProxyURL, s.DocumentRoot, s.ForceHTTPS, s.ForceHTTPSAuto,
			s.CompressionEnabled, s.GzipEnabled, s.ZstdEnabled, s.CacheControlEnabled, s.CacheControlValue, s.PageCache,
			s.PHPVersion, s.PHPMemoryLimit, s.PHPPostMaxSize, s.PHPUploadMaxSize, s.PHPMaxExecutionTime, s.PHPMaxInputVars,
			s.CreatedAt, s.CreatedAt)
		if err != nil {
			return conflict(err)
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO site_domains (site_id, domain, is_primary, redirect_to_primary, created_at) VALUES (?, ?, 1, 0, ?)",
			s.ID, s.Domain, s.CreatedAt)
		return conflict(err)
	})
}

// UpdateSite saves the site's settings. Identity fields (owner, slug,
// domain) are not changed here.
func (db *DB) UpdateSite(ctx context.Context, s *Site) error {
	res, err := db.ExecContext(ctx,
		`UPDATE sites SET proxy_url = ?, force_https = ?, force_https_auto = ?, compression_enabled = ?, gzip_enabled = ?, zstd_enabled = ?,
			cache_control_enabled = ?, cache_control_value = ?, page_cache = ?, php_version = ?, php_memory_limit = ?,
			php_post_max_size = ?, php_upload_max_filesize = ?, php_max_execution_time = ?, php_max_input_vars = ?, updated_at = ?
		 WHERE id = ?`,
		s.ProxyURL, s.ForceHTTPS, s.ForceHTTPSAuto, s.CompressionEnabled, s.GzipEnabled, s.ZstdEnabled,
		s.CacheControlEnabled, s.CacheControlValue, s.PageCache, s.PHPVersion, s.PHPMemoryLimit, s.PHPPostMaxSize,
		s.PHPUploadMaxSize, s.PHPMaxExecutionTime, s.PHPMaxInputVars, time.Now().UTC(), s.ID)
	return affected(res, err)
}

func (db *DB) DeleteSite(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", id)
	return affected(res, err)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func domainTaken(ctx context.Context, q queryer, domain string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT (SELECT COUNT(*) FROM site_domains WHERE domain = ?) + (SELECT COUNT(*) FROM sites WHERE domain = ?)",
		domain, domain).Scan(&n)
	return n > 0, err
}

// DomainTaken reports whether any site uses domain.
func (db *DB) DomainTaken(ctx context.Context, domain string) (bool, error) {
	return domainTaken(ctx, db, domain)
}

func (db *DB) SlugTaken(ctx context.Context, username, slug string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites WHERE username = ? AND slug = ?", username, slug).Scan(&n)
	return n > 0, err
}

const domainColumns = "id, site_id, domain, COALESCE(is_primary, 0), COALESCE(redirect_to_primary, 0)"

func scanDomain(sc scanner) (*SiteDomain, error) {
	var d SiteDomain
	if err := sc.Scan(&d.ID, &d.SiteID, &d.Domain, &d.IsPrimary, &d.RedirectToPrimary); err != nil {
		return nil, err
	}
	return &d, nil
}

// DomainsBySite returns every site's domains, primary first.
func (db *DB) DomainsBySite(ctx context.Context) (map[string][]*SiteDomain, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+domainColumns+" FROM site_domains ORDER BY site_id, is_primary DESC, domain")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]*SiteDomain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out[d.SiteID] = append(out[d.SiteID], d)
	}
	return out, rows.Err()
}

func (db *DB) ListDomains(ctx context.Context, siteID string) ([]*SiteDomain, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+domainColumns+" FROM site_domains WHERE site_id = ? ORDER BY is_primary DESC, domain", siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SiteDomain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) GetDomain(ctx context.Context, siteID string, id int64) (*SiteDomain, error) {
	d, err := scanDomain(db.QueryRowContext(ctx, "SELECT "+domainColumns+" FROM site_domains WHERE id = ? AND site_id = ?", id, siteID))
	return d, notFound(err)
}

// AddDomain attaches an extra domain to a site, enforcing the per-site
// limit and global uniqueness inside one transaction.
func (db *DB) AddDomain(ctx context.Context, d *SiteDomain, maxPerSite int) error {
	return db.InTx(ctx, func(tx *sql.Tx) error {
		if taken, err := domainTaken(ctx, tx, d.Domain); err != nil {
			return err
		} else if taken {
			return ErrConflict
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM site_domains WHERE site_id = ?", d.SiteID).Scan(&n); err != nil {
			return err
		}
		if n >= maxPerSite {
			return ErrLimit
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO site_domains (site_id, domain, is_primary, redirect_to_primary, created_at) VALUES (?, ?, 0, ?, ?)",
			d.SiteID, d.Domain, d.RedirectToPrimary, time.Now().UTC())
		if err != nil {
			return conflict(err)
		}
		d.ID, _ = res.LastInsertId()
		return nil
	})
}

func (db *DB) SetDomainRedirect(ctx context.Context, id int64, redirect bool) error {
	res, err := db.ExecContext(ctx, "UPDATE site_domains SET redirect_to_primary = ? WHERE id = ? AND is_primary = 0", redirect, id)
	return affected(res, err)
}

// SetPrimaryDomain makes domain id the site's primary domain and keeps
// sites.domain in sync.
func (db *DB) SetPrimaryDomain(ctx context.Context, siteID string, id int64) error {
	return db.InTx(ctx, func(tx *sql.Tx) error {
		var domain string
		err := tx.QueryRowContext(ctx, "SELECT domain FROM site_domains WHERE id = ? AND site_id = ?", id, siteID).Scan(&domain)
		if err != nil {
			return notFound(err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE site_domains SET is_primary = 0 WHERE site_id = ?", siteID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE site_domains SET is_primary = 1, redirect_to_primary = 0 WHERE id = ?", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE sites SET domain = ?, updated_at = ? WHERE id = ?", domain, time.Now().UTC(), siteID)
		return conflict(err)
	})
}

func (db *DB) DeleteDomain(ctx context.Context, siteID string, id int64) error {
	res, err := db.ExecContext(ctx, "DELETE FROM site_domains WHERE id = ? AND site_id = ? AND is_primary = 0", id, siteID)
	return affected(res, err)
}
