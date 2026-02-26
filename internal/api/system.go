package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
)

const (
	githubReleasesAPI  = "https://api.github.com/repos/rehmatworks/fastcp/releases/latest"
	updateCheckCacheTTL = 30 * time.Minute
)

// SystemService handles system operations
type SystemService struct {
	agent          *agent.Client
	currentVersion string
	phpInstallMu   sync.RWMutex
	phpInstalls    map[string]*PHPVersionInstallStatus
	updateMu       sync.RWMutex
	lastUpdateInfo *UpdateInfo
	lastUpdateAt   time.Time
}

type PHPVersionInstallStatus struct {
	Version    string `json:"version"`
	Status     string `json:"status"` // idle | running | success | failed
	Message    string `json:"message,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// NewSystemService creates a new system service
func NewSystemService(agent *agent.Client, version string) *SystemService {
	return &SystemService{
		agent:          agent,
		currentVersion: version,
		phpInstalls:    make(map[string]*PHPVersionInstallStatus),
	}
}

// GetStatus returns system status
func (s *SystemService) GetStatus(ctx context.Context) (*SystemStatus, error) {
	status, err := s.agent.GetSystemStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &SystemStatus{
		Hostname:             status.Hostname,
		OS:                   status.OS,
		Uptime:               status.Uptime,
		LoadAverage:          status.LoadAverage,
		MemoryTotal:          status.MemoryTotal,
		MemoryUsed:           status.MemoryUsed,
		DiskTotal:            status.DiskTotal,
		DiskUsed:             status.DiskUsed,
		PHPVersion:           status.PHPVersion,
		MySQLVersion:         status.MySQLVersion,
		CaddyVersion:         status.CaddyVersion,
		PHPAvailableVersions: status.PHPAvailableVersions,
		KernelVersion:        status.KernelVersion,
		Architecture:         status.Architecture,
		TotalUsers:           status.TotalUsers,
		TotalWebsites:        status.TotalWebsites,
		TotalDatabases:       status.TotalDatabases,
	}, nil
}

// GetServices returns system services status
func (s *SystemService) GetServices(ctx context.Context) ([]*Service, error) {
	services, err := s.agent.GetServices(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Service, len(services))
	for i, svc := range services {
		result[i] = &Service{
			Name:    svc.Name,
			Status:  svc.Status,
			Enabled: svc.Enabled,
		}
	}
	return result, nil
}

// GitHubRelease represents a GitHub release response
type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// CheckUpdate checks if a new version is available.
// Non-forced calls return cached results for a short TTL to avoid GitHub API rate limits.
func (s *SystemService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	if !force {
		s.updateMu.RLock()
		cached := s.lastUpdateInfo
		cachedAt := s.lastUpdateAt
		s.updateMu.RUnlock()
		if cached != nil && !cachedAt.IsZero() && time.Since(cachedAt) < updateCheckCacheTTL {
			copy := *cached
			return &copy, nil
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", githubReleasesAPI, nil)
	if err != nil {
		return s.fallbackUpdateInfo("Unable to create update-check request."), nil
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "FastCP/"+s.currentVersion)

	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("update check failed", "error", err)
		return s.fallbackUpdateInfo("Unable to reach GitHub release API right now."), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check returned non-200", "status_code", resp.StatusCode)
		return s.fallbackUpdateInfo(fmt.Sprintf("GitHub API returned status %d.", resp.StatusCode)), nil
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("update check response parse failed", "error", err)
		return s.fallbackUpdateInfo("Failed to parse GitHub release response."), nil
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(s.currentVersion, "v")

	updateAvailable := latestVersion != currentVersion && s.currentVersion != "dev"

	info := &UpdateInfo{
		CurrentVersion:  s.currentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		ReleaseNotes:    release.Body,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
	}
	s.updateMu.Lock()
	s.lastUpdateInfo = info
	s.lastUpdateAt = time.Now().UTC()
	s.updateMu.Unlock()
	return info, nil
}

func (s *SystemService) fallbackUpdateInfo(warning string) *UpdateInfo {
	s.updateMu.RLock()
	cached := s.lastUpdateInfo
	s.updateMu.RUnlock()
	if cached != nil {
		copy := *cached
		copy.Warning = warning
		return &copy
	}
	return &UpdateInfo{
		CurrentVersion:  s.currentVersion,
		LatestVersion:   s.currentVersion,
		UpdateAvailable: false,
		Warning:         warning,
	}
}

// GetMySQLConfig returns current MySQL tuning settings
func (s *SystemService) GetMySQLConfig(ctx context.Context) (*agent.MySQLConfig, error) {
	return s.agent.GetMySQLConfig(ctx)
}

// SetMySQLConfig applies new MySQL tuning settings and restarts MySQL
func (s *SystemService) SetMySQLConfig(ctx context.Context, cfg *agent.MySQLConfig) error {
	return s.agent.SetMySQLConfig(ctx, cfg)
}

// GetSSHConfig returns current SSH daemon settings
func (s *SystemService) GetSSHConfig(ctx context.Context) (*agent.SSHConfig, error) {
	return s.agent.GetSSHConfig(ctx)
}

// SetSSHConfig applies SSH daemon settings and reloads SSH service
func (s *SystemService) SetSSHConfig(ctx context.Context, cfg *agent.SSHConfig) error {
	return s.agent.SetSSHConfig(ctx, cfg)
}

// GetPHPDefaultConfig returns configured default PHP version for new sites
func (s *SystemService) GetPHPDefaultConfig(ctx context.Context) (*agent.PHPDefaultConfig, error) {
	return s.agent.GetPHPDefaultConfig(ctx)
}

// SetPHPDefaultConfig updates default PHP version for new sites
func (s *SystemService) SetPHPDefaultConfig(ctx context.Context, cfg *agent.PHPDefaultConfig) error {
	return s.agent.SetPHPDefaultConfig(ctx, cfg)
}

func (s *SystemService) InstallPHPVersion(ctx context.Context, version string) (*PHPVersionInstallStatus, error) {
	v := strings.TrimSpace(version)
	if v == "" {
		return nil, fmt.Errorf("php version is required")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	s.phpInstallMu.Lock()
	if current, ok := s.phpInstalls[v]; ok && current.Status == "running" {
		copy := *current
		s.phpInstallMu.Unlock()
		return &copy, nil
	}
	status := &PHPVersionInstallStatus{
		Version:   v,
		Status:    "running",
		Message:   fmt.Sprintf("PHP %s installation started.", v),
		StartedAt: now,
	}
	s.phpInstalls[v] = status
	s.phpInstallMu.Unlock()

	go func(version string) {
		jobCtx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
		defer cancel()

		err := s.agent.InstallPHPVersion(jobCtx, version)
		finishedAt := time.Now().UTC().Format(time.RFC3339)

		s.phpInstallMu.Lock()
		defer s.phpInstallMu.Unlock()

		current, ok := s.phpInstalls[version]
		if !ok {
			current = &PHPVersionInstallStatus{Version: version, StartedAt: now}
			s.phpInstalls[version] = current
		}
		current.FinishedAt = finishedAt
		if err != nil {
			current.Status = "failed"
			current.Message = err.Error()
			return
		}
		current.Status = "success"
		current.Message = fmt.Sprintf("PHP %s installed successfully.", version)
	}(v)

	copy := *status
	return &copy, nil
}

func (s *SystemService) GetPHPInstallStatus(version string) *PHPVersionInstallStatus {
	v := strings.TrimSpace(version)
	if v == "" {
		return &PHPVersionInstallStatus{Status: "idle", Message: "php version is required"}
	}
	s.phpInstallMu.RLock()
	defer s.phpInstallMu.RUnlock()
	if status, ok := s.phpInstalls[v]; ok {
		copy := *status
		return &copy
	}
	return &PHPVersionInstallStatus{
		Version: v,
		Status:  "idle",
		Message: "No installation job found for this PHP version.",
	}
}

// GetCaddyConfig returns current Caddy performance/logging settings
func (s *SystemService) GetCaddyConfig(ctx context.Context) (*agent.CaddyConfig, error) {
	return s.agent.GetCaddyConfig(ctx)
}

// SetCaddyConfig applies Caddy performance/logging settings and reloads Caddy
func (s *SystemService) SetCaddyConfig(ctx context.Context, cfg *agent.CaddyConfig) error {
	return s.agent.SetCaddyConfig(ctx, cfg)
}

func (s *SystemService) GetFirewallStatus(ctx context.Context) (*agent.FirewallStatus, error) {
	return s.agent.GetFirewallStatus(ctx)
}

func (s *SystemService) InstallFirewall(ctx context.Context) error {
	return s.agent.InstallFirewall(ctx)
}

func (s *SystemService) SetFirewallEnabled(ctx context.Context, enabled bool) error {
	return s.agent.SetFirewallEnabled(ctx, enabled)
}

func (s *SystemService) FirewallAllowPort(ctx context.Context, req *agent.FirewallRuleRequest) error {
	return s.agent.FirewallAllowPort(ctx, req)
}

func (s *SystemService) FirewallDenyPort(ctx context.Context, req *agent.FirewallRuleRequest) error {
	return s.agent.FirewallDenyPort(ctx, req)
}

func (s *SystemService) FirewallDeleteRule(ctx context.Context, req *agent.FirewallRuleRequest) error {
	return s.agent.FirewallDeleteRule(ctx, req)
}

// PerformUpdate downloads and installs the latest version
func (s *SystemService) PerformUpdate(ctx context.Context, targetVersion string) error {
	return s.agent.PerformUpdate(ctx, targetVersion)
}
