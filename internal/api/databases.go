package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"` // Optional: auto-generated if not provided
	SiteID   string `json:"site_id,omitempty"`  // Optional: link to a site
	Type     string `json:"type,omitempty"`     // mysql or postgresql, defaults to mysql
}

// listDatabases returns all databases for the current user
func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var databases []*models.Database
	if claims.Role == "admin" {
		databases = s.dbManager.List("")
	} else {
		databases = s.dbManager.List(claims.UserID)
	}

	// Don't return passwords in list
	for _, db := range databases {
		db.Password = ""
	}

	s.success(w, map[string]interface{}{
		"databases": databases,
		"total":     len(databases),
	})
}

// createDatabase creates a new database
func (s *Server) createDatabase(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		s.error(w, http.StatusBadRequest, "database name is required")
		return
	}

	if req.Username == "" {
		// Use database name as username if not provided
		req.Username = req.Name
	}

	// Validate name (alphanumeric and underscore only)
	if !isValidDatabaseName(req.Name) {
		s.error(w, http.StatusBadRequest, "database name can only contain letters, numbers, and underscores")
		return
	}

	if !isValidDatabaseName(req.Username) {
		s.error(w, http.StatusBadRequest, "username can only contain letters, numbers, and underscores")
		return
	}

	db := &models.Database{
		UserID:   claims.UserID,
		SiteID:   req.SiteID,
		Name:     req.Name,
		Username: req.Username,
		Password: req.Password,
		Type:     req.Type,
		Host:     "localhost",
	}

	created, err := s.dbManager.Create(db)
	if err != nil {
		if err == database.ErrDatabaseExists {
			s.error(w, http.StatusConflict, "database already exists")
			return
		}
		if err == database.ErrUserExists {
			s.error(w, http.StatusConflict, "database user already exists")
			return
		}
		s.logger.Error("failed to create database", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to create database: "+err.Error())
		return
	}

	s.logger.Info("database created", "id", created.ID, "name", created.Name, "user", claims.Username)
	s.json(w, http.StatusCreated, created)
}

// getDatabase returns a single database
func (s *Server) getDatabase(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	db, err := s.dbManager.Get(id)
	if err != nil {
		if err == database.ErrDatabaseNotFound {
			s.error(w, http.StatusNotFound, "database not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get database")
		return
	}

	// Check ownership
	if claims.Role != "admin" && db.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Don't return password
	db.Password = ""
	s.success(w, db)
}

// deleteDatabase deletes a database
func (s *Server) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	db, err := s.dbManager.Get(id)
	if err != nil {
		if err == database.ErrDatabaseNotFound {
			s.error(w, http.StatusNotFound, "database not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get database")
		return
	}

	// Check ownership
	if claims.Role != "admin" && db.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.dbManager.Delete(id); err != nil {
		s.logger.Error("failed to delete database", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to delete database")
		return
	}

	s.logger.Info("database deleted", "id", id, "name", db.Name, "user", claims.Username)
	s.success(w, map[string]string{"message": "database deleted"})
}

// resetDatabasePassword resets a database user's password
func (s *Server) resetDatabasePassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	db, err := s.dbManager.Get(id)
	if err != nil {
		if err == database.ErrDatabaseNotFound {
			s.error(w, http.StatusNotFound, "database not found")
			return
		}
		s.error(w, http.StatusInternalServerError, "failed to get database")
		return
	}

	// Check ownership
	if claims.Role != "admin" && db.UserID != claims.UserID {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password == "" {
		s.error(w, http.StatusBadRequest, "password is required")
		return
	}

	if err := s.dbManager.UpdatePassword(id, req.Password); err != nil {
		s.logger.Error("failed to update database password", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	s.logger.Info("database password reset", "id", id, "name", db.Name, "user", claims.Username)
	s.success(w, map[string]string{"message": "password updated"})
}

// getDatabaseStatus returns the MySQL server status
func (s *Server) getDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	status := s.dbManager.GetStatus()
	s.success(w, status)
}

// installMySQL starts MySQL installation asynchronously
func (s *Server) installMySQL(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	// Check if installation is already in progress
	if s.dbManager.IsInstalling("mysql") {
		s.error(w, http.StatusConflict, "MySQL installation already in progress")
		return
	}

	// Check if MySQL is already installed and running
	if s.dbManager.IsMySQLInstalled() && s.dbManager.IsMySQLRunning() {
		s.logger.Info("MySQL already installed, attempting to adopt", "user", claims.Username)

		// Try to adopt the existing installation (this is quick, can be sync)
		if err := s.dbManager.AdoptMySQL(); err != nil {
			s.logger.Error("failed to adopt existing MySQL", "error", err)
			s.error(w, http.StatusInternalServerError, "MySQL is installed but FastCP cannot connect to it. Please ensure MySQL is running and accessible: "+err.Error())
			return
		}

		s.logger.Info("MySQL adopted successfully", "user", claims.Username)
		s.success(w, map[string]interface{}{
			"message": "Existing MySQL installation configured successfully",
			"status":  "completed",
		})
		return
	}

	// Start async installation
	s.logger.Info("starting MySQL installation", "user", claims.Username)

	if err := s.dbManager.InstallDatabaseAsync("mysql"); err != nil {
		s.logger.Error("failed to start MySQL installation", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to start MySQL installation: "+err.Error())
		return
	}

	s.json(w, http.StatusAccepted, map[string]interface{}{
		"message": "MySQL installation started",
		"status":  "installing",
	})
}

// installPostgreSQL starts PostgreSQL installation asynchronously
func (s *Server) installPostgreSQL(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	// Check if installation is already in progress
	if s.dbManager.IsInstalling("postgresql") {
		s.error(w, http.StatusConflict, "PostgreSQL installation already in progress")
		return
	}

	// Check if PostgreSQL is already installed and running
	if s.dbManager.IsPostgreSQLInstalled() && s.dbManager.IsPostgreSQLRunning() {
		s.logger.Info("PostgreSQL already installed, attempting to adopt", "user", claims.Username)

		// Try to adopt the existing installation (this is quick, can be sync)
		if err := s.dbManager.AdoptPostgreSQL(); err != nil {
			s.logger.Error("failed to adopt existing PostgreSQL", "error", err)
			s.error(w, http.StatusInternalServerError, "PostgreSQL is installed but FastCP cannot connect to it. Please ensure PostgreSQL is running and accessible: "+err.Error())
			return
		}

		s.logger.Info("PostgreSQL adopted successfully", "user", claims.Username)
		s.success(w, map[string]interface{}{
			"message": "Existing PostgreSQL installation configured successfully",
			"status":  "completed",
		})
		return
	}

	// Start async installation
	s.logger.Info("starting PostgreSQL installation", "user", claims.Username)

	if err := s.dbManager.InstallDatabaseAsync("postgresql"); err != nil {
		s.logger.Error("failed to start PostgreSQL installation", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to start PostgreSQL installation: "+err.Error())
		return
	}

	s.json(w, http.StatusAccepted, map[string]interface{}{
		"message": "PostgreSQL installation started",
		"status":  "installing",
	})
}

// getMySQLInstallStatus returns the current MySQL installation status
func (s *Server) getMySQLInstallStatus(w http.ResponseWriter, r *http.Request) {
	status := s.dbManager.GetInstallStatus("mysql")
	s.success(w, status)
}

// getPostgreSQLInstallStatus returns the current PostgreSQL installation status
func (s *Server) getPostgreSQLInstallStatus(w http.ResponseWriter, r *http.Request) {
	status := s.dbManager.GetInstallStatus("postgresql")
	s.success(w, status)
}

// isValidDatabaseName checks if a name is valid for MySQL
func isValidDatabaseName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}
