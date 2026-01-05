package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

var startTime = time.Now()

// getSystemMemory reads system memory info from /proc/meminfo (Linux)
func getSystemMemory() (total, used int64) {
	if runtime.GOOS != "linux" {
		// Fallback to Go runtime stats for non-Linux
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		return int64(mem.Sys), int64(mem.Alloc)
	}

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var memTotal, memAvailable, memFree, buffers, cached int64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}

		// Values in /proc/meminfo are in KB
		switch fields[0] {
		case "MemTotal:":
			memTotal = value * 1024 // Convert to bytes
		case "MemAvailable:":
			memAvailable = value * 1024
		case "MemFree:":
			memFree = value * 1024
		case "Buffers:":
			buffers = value * 1024
		case "Cached:":
			cached = value * 1024
		}
	}

	// Calculate used memory
	// If MemAvailable is present (Linux 3.14+), use it
	if memAvailable > 0 {
		used = memTotal - memAvailable
	} else {
		// Fallback: used = total - free - buffers - cached
		used = memTotal - memFree - buffers - cached
	}

	return memTotal, used
}

// getStats returns dashboard statistics
func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	totalSites, activeSites := s.siteManager.GetStats()
	phpInstances := s.phpManager.GetStatus()

	memTotal, memUsed := getSystemMemory()

	stats := models.Stats{
		TotalSites:   totalSites,
		ActiveSites:  activeSites,
		TotalUsers:   1, // Hardcoded for now
		PHPInstances: len(phpInstances),
		MemoryUsage:  memUsed,
		MemoryTotal:  memTotal,
		Uptime:       int64(time.Since(startTime).Seconds()),
	}

	s.success(w, stats)
}

// getConfig returns the current configuration (admin only)
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()

	// Don't expose sensitive fields
	safeCfg := map[string]interface{}{
		"data_dir":       cfg.DataDir,
		"sites_dir":      cfg.SitesDir,
		"log_dir":        cfg.LogDir,
		"listen_addr":    cfg.ListenAddr,
		"proxy_port":     cfg.ProxyPort,
		"proxy_ssl_port": cfg.ProxySSLPort,
		"php_versions":   cfg.PHPVersions,
	}

	s.success(w, safeCfg)
}

// updateConfig updates the configuration (admin only)
func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var updates struct {
		PHPVersions []models.PHPVersionConfig `json:"php_versions,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := config.Get()

	if updates.PHPVersions != nil {
		cfg.PHPVersions = updates.PHPVersions
	}

	config.Update(cfg)

	if err := config.Save(""); err != nil {
		s.logger.Error("failed to save config", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}

	s.logger.Info("configuration updated", "user", claims.Username)
	s.success(w, map[string]string{"message": "configuration updated"})
}

// reloadAll reloads all configurations
func (s *Server) reloadAll(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	// Reload PHP instances
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Error("failed to reload PHP instances", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to reload PHP instances")
		return
	}

	s.logger.Info("all configurations reloaded", "user", claims.Username)
	s.success(w, map[string]string{"message": "configurations reloaded"})
}

// In-memory API keys storage (replace with database in production)
var apiKeys = make(map[string]*models.APIKey)

// listAPIKeys returns all API keys (admin only)
func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys := make([]*models.APIKey, 0, len(apiKeys))
	for _, key := range apiKeys {
		// Don't expose the actual key value
		safeCopy := *key
		safeCopy.Key = key.Key[:12] + "..." // Show only prefix
		keys = append(keys, &safeCopy)
	}

	s.success(w, map[string]interface{}{
		"api_keys": keys,
		"total":    len(keys),
	})
}

// createAPIKey creates a new API key (admin only)
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		s.error(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.Permissions == nil {
		req.Permissions = []string{"sites:read", "sites:write"}
	}

	apiKey, err := auth.GenerateAPIKey(req.Name, claims.UserID, req.Permissions)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to generate API key")
		return
	}

	apiKeys[apiKey.ID] = apiKey

	s.logger.Info("API key created", "id", apiKey.ID, "name", apiKey.Name, "user", claims.Username)
	
	// Return the full key only on creation
	s.json(w, http.StatusCreated, apiKey)
}

// deleteAPIKey deletes an API key (admin only)
func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims := middleware.GetClaims(r)

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		s.error(w, http.StatusBadRequest, "invalid API key ID")
		return
	}

	if _, exists := apiKeys[id]; !exists {
		s.error(w, http.StatusNotFound, "API key not found")
		return
	}

	delete(apiKeys, id)

	s.logger.Info("API key deleted", "id", id, "user", claims.Username)
	s.success(w, map[string]string{"message": "API key deleted"})
}

