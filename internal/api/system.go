package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
)

const (
	githubReleasesAPI = "https://api.github.com/repos/rehmatworks/fastcp/releases/latest"
)

// SystemService handles system operations
type SystemService struct {
	agent          *agent.Client
	currentVersion string
}

// NewSystemService creates a new system service
func NewSystemService(agent *agent.Client, version string) *SystemService {
	return &SystemService{
		agent:          agent,
		currentVersion: version,
	}
}

// GetStatus returns system status
func (s *SystemService) GetStatus(ctx context.Context) (*SystemStatus, error) {
	status, err := s.agent.GetSystemStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &SystemStatus{
		Hostname:     status.Hostname,
		OS:           status.OS,
		Uptime:       status.Uptime,
		LoadAverage:  status.LoadAverage,
		MemoryTotal:  status.MemoryTotal,
		MemoryUsed:   status.MemoryUsed,
		DiskTotal:    status.DiskTotal,
		DiskUsed:     status.DiskUsed,
		PHPVersion:   status.PHPVersion,
		MySQLVersion: status.MySQLVersion,
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

// CheckUpdate checks if a new version is available
func (s *SystemService) CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", githubReleasesAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "FastCP/"+s.currentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(s.currentVersion, "v")

	updateAvailable := latestVersion != currentVersion && s.currentVersion != "dev"

	return &UpdateInfo{
		CurrentVersion:  s.currentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		ReleaseNotes:    release.Body,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
	}, nil
}

// PerformUpdate downloads and installs the latest version
func (s *SystemService) PerformUpdate(ctx context.Context, targetVersion string) error {
	return s.agent.PerformUpdate(ctx, targetVersion)
}
