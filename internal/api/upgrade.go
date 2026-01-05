package api

import (
	"net/http"

	"github.com/rehmatworks/fastcp/internal/middleware"
)

// getVersion returns the current version and checks for updates
func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	if s.upgradeManager == nil {
		s.error(w, http.StatusInternalServerError, "upgrade manager not initialized")
		return
	}

	result, err := s.upgradeManager.CheckForUpdates(r.Context())
	if err != nil {
		s.logger.Warn("failed to check for updates", "error", err)
		// Return current version even if check fails
		s.success(w, map[string]interface{}{
			"current_version":  s.upgradeManager.GetCurrentVersion(),
			"update_available": false,
			"check_error":      err.Error(),
		})
		return
	}

	s.success(w, result)
}

// startUpgrade initiates the upgrade process
func (s *Server) startUpgrade(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if s.upgradeManager == nil {
		s.error(w, http.StatusInternalServerError, "upgrade manager not initialized")
		return
	}

	// Check if already upgrading
	if s.upgradeManager.IsUpgrading() {
		s.error(w, http.StatusConflict, "upgrade already in progress")
		return
	}

	// Start async upgrade
	s.logger.Info("starting upgrade", "user", claims.Username)

	if err := s.upgradeManager.StartUpgrade(r.Context()); err != nil {
		s.logger.Error("failed to start upgrade", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to start upgrade: "+err.Error())
		return
	}

	s.json(w, http.StatusAccepted, map[string]interface{}{
		"message": "Upgrade started",
		"status":  "upgrading",
	})
}

// getUpgradeStatus returns the current upgrade status
func (s *Server) getUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if s.upgradeManager == nil {
		s.error(w, http.StatusInternalServerError, "upgrade manager not initialized")
		return
	}

	status := s.upgradeManager.GetUpgradeStatus()
	s.success(w, status)
}

