package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
	"github.com/rehmatworks/fastcp/internal/sites"
)

// CreateSiteRequest represents a request to create a site
type CreateSiteRequest struct {
	Name        string            `json:"name"`
	Domain      string            `json:"domain"`
	Aliases     []string          `json:"aliases,omitempty"`
	PHPVersion  string            `json:"php_version"`
	PublicPath  string            `json:"public_path,omitempty"`
	AppType     string            `json:"app_type"` // blank, wordpress
	WorkerMode  bool              `json:"worker_mode"`
	WorkerFile  string            `json:"worker_file,omitempty"`
	WorkerNum   int               `json:"worker_num,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// UpdateSiteRequest represents a request to update a site
type UpdateSiteRequest struct {
	Name        string            `json:"name,omitempty"`
	Domain      string            `json:"domain,omitempty"`
	Aliases     []string          `json:"aliases,omitempty"`
	PHPVersion  string            `json:"php_version,omitempty"`
	PublicPath  string            `json:"public_path,omitempty"`
	WorkerMode  bool              `json:"worker_mode"`
	WorkerFile  string            `json:"worker_file,omitempty"`
	WorkerNum   int               `json:"worker_num,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// listSites returns all sites
func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	
	var sitesList []*models.Site
	if claims.Role == "admin" {
		sitesList = s.siteManager.List("")
	} else {
		sitesList = s.siteManager.List(claims.UserID)
	}

	s.success(w, map[string]interface{}{
		"sites": sitesList,
		"total": len(sitesList),
	})
}

// createSite creates a new site
func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var req CreateSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate required fields
	if req.Domain == "" {
		s.error(w, http.StatusBadRequest, "domain is required")
		return
	}
	if req.PHPVersion == "" {
		s.error(w, http.StatusBadRequest, "php_version is required")
		return
	}

	// Default app type to blank
	appType := req.AppType
	if appType == "" {
		appType = "blank"
	}

	site := &models.Site{
		UserID:      claims.UserID,
		Name:        req.Name,
		Domain:      req.Domain,
		Aliases:     req.Aliases,
		PHPVersion:  req.PHPVersion,
		PublicPath:  req.PublicPath,
		AppType:     appType,
		WorkerMode:  req.WorkerMode,
		WorkerFile:  req.WorkerFile,
		WorkerNum:   req.WorkerNum,
		Environment: req.Environment,
		SSL:         true,
	}

	if site.Name == "" {
		site.Name = site.Domain
	}

	created, err := s.siteManager.Create(site)
	if err != nil {
		if err == sites.ErrDomainExists {
			s.error(w, http.StatusConflict, "domain already exists")
			return
		}
		if err == sites.ErrInvalidPHPVersion {
			s.error(w, http.StatusBadRequest, "invalid PHP version")
			return
		}
		s.logger.Error("failed to create site", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to create site")
		return
	}

	// If WordPress, install it
	if appType == "wordpress" {
		s.logger.Info("installing WordPress", "site", created.Domain)
		dbInfo, err := s.siteManager.InstallWordPress(created, s.dbManager)
		if err != nil {
			s.logger.Error("failed to install WordPress", "error", err)
			// Site was created but WordPress installation failed
			// Don't fail the entire request, just log it
		} else {
			// Update site with database ID
			created.DatabaseID = dbInfo.ID
			s.siteManager.Update(created.ID, created)
		}
	}

	// Reload PHP instances to apply changes
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances", "error", err)
	}

	s.logger.Info("site created", "id", created.ID, "domain", created.Domain, "app", appType, "user", claims.Username)
	s.json(w, http.StatusCreated, created)
}

// getSite returns a single site
func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	site, err := s.siteManager.Get(id)
	if err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get site")
		return
	}

	// Check ownership (unless admin)
	if claims.Role != "admin" && site.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	s.success(w, site)
}

// updateSite updates an existing site
func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	// Check ownership
	site, err := s.siteManager.Get(id)
	if err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get site")
		return
	}

	if claims.Role != "admin" && site.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	var req UpdateSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate worker mode configuration
	if req.WorkerMode {
		if req.WorkerFile == "" {
			s.error(w, http.StatusBadRequest, "worker_file is required when worker_mode is enabled")
			return
		}
		// Check if worker file exists
		publicPath := site.PublicPath
		if req.PublicPath != "" {
			publicPath = req.PublicPath
		}
		workerPath := filepath.Join(site.RootPath, publicPath, req.WorkerFile)
		if _, err := os.Stat(workerPath); os.IsNotExist(err) {
			s.error(w, http.StatusBadRequest, fmt.Sprintf("worker file not found: %s", workerPath))
			return
		}
	}

	updates := &models.Site{
		Name:        req.Name,
		Domain:      req.Domain,
		Aliases:     req.Aliases,
		PHPVersion:  req.PHPVersion,
		PublicPath:  req.PublicPath,
		WorkerMode:  req.WorkerMode,
		WorkerFile:  req.WorkerFile,
		WorkerNum:   req.WorkerNum,
		Environment: req.Environment,
	}

	updated, err := s.siteManager.Update(id, updates)
	if err != nil {
		if err == sites.ErrDomainExists {
			s.error(w, http.StatusConflict, "domain already exists")
			return
		}
		if err == sites.ErrInvalidPHPVersion {
			s.error(w, http.StatusBadRequest, "invalid PHP version")
			return
		}
		s.logger.Error("failed to update site", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to update site")
		return
	}

	// Reload PHP instances to apply changes
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances", "error", err)
	}

	s.logger.Info("site updated", "id", id, "user", claims.Username)
	s.success(w, updated)
}

// deleteSite removes a site
func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	// Check ownership
	site, err := s.siteManager.Get(id)
	if err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get site")
		return
	}

	if claims.Role != "admin" && site.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.siteManager.Delete(id); err != nil {
		s.logger.Error("failed to delete site", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to delete site")
		return
	}

	// Reload PHP instances to apply changes
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances", "error", err)
	}

	s.logger.Info("site deleted", "id", id, "domain", site.Domain, "user", claims.Username)
	s.success(w, map[string]string{"message": "site deleted"})
}

// suspendSite suspends a site
func (s *Server) suspendSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	// Only admin can suspend
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if err := s.siteManager.Suspend(id); err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to suspend site")
		return
	}

	// Reload PHP instances to apply changes
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances", "error", err)
	}

	s.logger.Info("site suspended", "id", id, "user", claims.Username)
	s.success(w, map[string]string{"message": "site suspended"})
}

// unsuspendSite reactivates a suspended site
func (s *Server) unsuspendSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	// Only admin can unsuspend
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if err := s.siteManager.Unsuspend(id); err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to unsuspend site")
		return
	}

	// Reload PHP instances to apply changes
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances", "error", err)
	}

	s.logger.Info("site unsuspended", "id", id, "user", claims.Username)
	s.success(w, map[string]string{"message": "site unsuspended"})
}

// restartSiteWorkers restarts workers for a specific site
func (s *Server) restartSiteWorkers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	site, err := s.siteManager.Get(id)
	if err != nil {
		if err == sites.ErrSiteNotFound {
			s.error(w, http.StatusNotFound, "site not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get site")
		return
	}

	if claims.Role != "admin" && site.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Restart workers for this site's PHP version
	if err := s.phpManager.RestartWorkers(site.PHPVersion); err != nil {
		s.logger.Warn("failed to restart workers", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to restart workers")
		return
	}

	s.logger.Info("workers restarted for site", "id", id, "php_version", site.PHPVersion, "user", claims.Username)
	s.success(w, map[string]string{"message": "workers restarted"})
}

