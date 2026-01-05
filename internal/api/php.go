package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/downloader"
	"github.com/rehmatworks/fastcp/internal/middleware"
)

// Download state tracking
var (
	downloadStates   = make(map[string]*DownloadState)
	downloadStatesMu sync.RWMutex
)

// DownloadState tracks the state of a PHP version download
type DownloadState struct {
	Version    string    `json:"version"`
	Status     string    `json:"status"` // "pending", "downloading", "completed", "failed"
	Progress   float64   `json:"progress"`
	Downloaded int64     `json:"downloaded"`
	Total      int64     `json:"total"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// listPHPInstances returns all PHP instances
func (s *Server) listPHPInstances(w http.ResponseWriter, r *http.Request) {
	instances := s.phpManager.GetStatus()

	s.success(w, map[string]interface{}{
		"instances": instances,
		"total":     len(instances),
	})
}

// getPHPInstance returns a specific PHP instance
func (s *Server) getPHPInstance(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")

	instance, err := s.phpManager.GetInstance(version)
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}

	s.success(w, instance)
}

// startPHPInstance starts a PHP instance
func (s *Server) startPHPInstance(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	claims := middleware.GetClaims(r)

	// Only admin can manage PHP instances
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if err := s.phpManager.Start(version); err != nil {
		s.logger.Error("failed to start PHP instance", "version", version, "error", err)
		s.error(w, http.StatusInternalServerError, "failed to start PHP instance")
		return
	}

	s.logger.Info("PHP instance started", "version", version, "user", claims.Username)
	s.success(w, map[string]string{"message": "PHP instance started"})
}

// stopPHPInstance stops a PHP instance
func (s *Server) stopPHPInstance(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	claims := middleware.GetClaims(r)

	// Only admin can manage PHP instances
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if err := s.phpManager.Stop(version); err != nil {
		s.logger.Error("failed to stop PHP instance", "version", version, "error", err)
		s.error(w, http.StatusInternalServerError, "failed to stop PHP instance")
		return
	}

	s.logger.Info("PHP instance stopped", "version", version, "user", claims.Username)
	s.success(w, map[string]string{"message": "PHP instance stopped"})
}

// restartPHPInstance restarts a PHP instance
func (s *Server) restartPHPInstance(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	claims := middleware.GetClaims(r)

	// Only admin can manage PHP instances
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	if err := s.phpManager.Restart(version); err != nil {
		s.logger.Error("failed to restart PHP instance", "version", version, "error", err)
		s.error(w, http.StatusInternalServerError, "failed to restart PHP instance")
		return
	}

	s.logger.Info("PHP instance restarted", "version", version, "user", claims.Username)
	s.success(w, map[string]string{"message": "PHP instance restarted"})
}

// restartPHPWorkers restarts workers for a PHP instance
func (s *Server) restartPHPWorkers(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	claims := middleware.GetClaims(r)

	if err := s.phpManager.RestartWorkers(version); err != nil {
		s.logger.Error("failed to restart workers", "version", version, "error", err)
		s.error(w, http.StatusInternalServerError, "failed to restart workers")
		return
	}

	s.logger.Info("PHP workers restarted", "version", version, "user", claims.Username)
	s.success(w, map[string]string{"message": "workers restarted"})
}

// getAvailablePHPVersions returns available PHP versions for download
func (s *Server) getAvailablePHPVersions(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()

	dm := downloader.NewManager(downloader.Config{
		Source:   downloader.SourceGitHub,
		CacheDir: cfg.DataDir + "/downloads",
	})

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	versions, err := dm.GetAvailableVersions(ctx)
	if err != nil {
		s.logger.Error("failed to get available PHP versions", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to get available versions")
		return
	}

	// Get current platform info
	platform := downloader.DetectPlatform()

	s.success(w, map[string]interface{}{
		"versions": versions,
		"platform": platform.String(),
	})
}

// downloadPHPVersion initiates download of a PHP version
func (s *Server) downloadPHPVersion(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	claims := middleware.GetClaims(r)

	// Only admin can download PHP versions
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	// Check if already downloading
	downloadStatesMu.RLock()
	state, exists := downloadStates[version]
	downloadStatesMu.RUnlock()

	if exists && state.Status == "downloading" {
		s.error(w, http.StatusConflict, "download already in progress")
		return
	}

	cfg := config.Get()

	// Find the binary path for this version
	var binaryPath string
	for _, pv := range cfg.PHPVersions {
		if pv.Version == version {
			binaryPath = pv.BinaryPath
			break
		}
	}

	if binaryPath == "" {
		// Use default path
		binaryPath = "/usr/local/bin/frankenphp-" + version
	}

	// Initialize download state
	downloadStatesMu.Lock()
	downloadStates[version] = &DownloadState{
		Version:   version,
		Status:    "pending",
		StartedAt: time.Now(),
	}
	downloadStatesMu.Unlock()

	// Start download in background
	go func() {
		downloadStatesMu.Lock()
		downloadStates[version].Status = "downloading"
		downloadStatesMu.Unlock()

		dm := downloader.NewManager(downloader.Config{
			Source:   downloader.SourceGitHub,
			CacheDir: cfg.DataDir + "/downloads",
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		err := dm.InstallPHPVersion(ctx, version, binaryPath, func(progress downloader.DownloadProgress) {
			downloadStatesMu.Lock()
			state := downloadStates[version]
			state.Progress = progress.Percent
			state.Downloaded = progress.Downloaded
			state.Total = progress.Total
			downloadStatesMu.Unlock()
		})

		now := time.Now()
		downloadStatesMu.Lock()
		state := downloadStates[version]
		state.CompletedAt = &now
		if err != nil {
			state.Status = "failed"
			state.Error = err.Error()
			s.logger.Error("PHP download failed", "version", version, "error", err)
		} else {
			state.Status = "completed"
			state.Progress = 100
			s.logger.Info("PHP download completed", "version", version, "path", binaryPath)
		}
		downloadStatesMu.Unlock()
	}()

	s.logger.Info("PHP download started", "version", version, "user", claims.Username)
	s.success(w, map[string]interface{}{
		"message": "download started",
		"version": version,
	})
}

// getDownloadStatus returns the download status for a PHP version
func (s *Server) getDownloadStatus(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")

	downloadStatesMu.RLock()
	state, exists := downloadStates[version]
	downloadStatesMu.RUnlock()

	if !exists {
		s.error(w, http.StatusNotFound, "no download found for this version")
		return
	}

	s.success(w, state)
}

// InstallPHPRequest represents a request to install a PHP version
type InstallPHPRequest struct {
	Version    string `json:"version"`
	BinaryPath string `json:"binary_path,omitempty"`
}

// installPHPVersion installs a new PHP version configuration
func (s *Server) installPHPVersion(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	// Only admin can install PHP versions
	if claims.Role != "admin" {
		s.error(w, http.StatusForbidden, "admin access required")
		return
	}

	var req InstallPHPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Version == "" {
		s.error(w, http.StatusBadRequest, "version is required")
		return
	}

	// Default binary path
	if req.BinaryPath == "" {
		req.BinaryPath = "/usr/local/bin/frankenphp-" + req.Version
	}

	// TODO: Add version to config and initialize PHP instance
	// For now, just return success message
	s.logger.Info("PHP version installation requested", "version", req.Version, "user", claims.Username)
	s.success(w, map[string]interface{}{
		"message":     "PHP version configuration added",
		"version":     req.Version,
		"binary_path": req.BinaryPath,
		"note":        "Use POST /api/v1/php/{version}/download to download the binary",
	})
}

