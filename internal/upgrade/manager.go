package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	VersionAPIURL = "https://www.fastcp.org/api/version"
	BinaryPath    = "/usr/local/bin/fastcp"
)

var (
	ErrUpgradeInProgress = errors.New("upgrade already in progress")
	ErrNoUpdateAvailable = errors.New("no update available")
	ErrUnsupportedArch   = errors.New("unsupported architecture")
)

// VersionInfo represents the response from fastcp.org API
type VersionInfo struct {
	Version     string            `json:"version"`
	ReleaseName string            `json:"release_name"`
	PublishedAt string            `json:"published_at"`
	ReleaseURL  string            `json:"release_url"`
	Changelog   string            `json:"changelog"`
	Downloads   map[string]string `json:"downloads"`
}

// UpgradeStatus represents the current upgrade status
type UpgradeStatus struct {
	InProgress  bool      `json:"in_progress"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	Message     string    `json:"message,omitempty"`
	Progress    float64   `json:"progress,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// VersionCheckResult represents the result of a version check
type VersionCheckResult struct {
	CurrentVersion  string       `json:"current_version"`
	LatestVersion   string       `json:"latest_version"`
	UpdateAvailable bool         `json:"update_available"`
	ReleaseName     string       `json:"release_name,omitempty"`
	ReleaseURL      string       `json:"release_url,omitempty"`
	Changelog       string       `json:"changelog,omitempty"`
	PublishedAt     string       `json:"published_at,omitempty"`
}

// Manager handles version checking and upgrades
type Manager struct {
	currentVersion string
	httpClient     *http.Client
	status         *UpgradeStatus
	mu             sync.RWMutex
	dataDir        string
}

// NewManager creates a new upgrade manager
func NewManager(currentVersion, dataDir string) *Manager {
	return &Manager{
		currentVersion: currentVersion,
		dataDir:        dataDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		status: &UpgradeStatus{},
	}
}

// GetCurrentVersion returns the current installed version
func (m *Manager) GetCurrentVersion() string {
	return m.currentVersion
}

// CheckForUpdates checks if a new version is available
func (m *Manager) CheckForUpdates(ctx context.Context) (*VersionCheckResult, error) {
	versionInfo, err := m.fetchVersionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch version info: %w", err)
	}

	result := &VersionCheckResult{
		CurrentVersion:  m.currentVersion,
		LatestVersion:   versionInfo.Version,
		UpdateAvailable: m.isNewerVersion(versionInfo.Version),
		ReleaseName:     versionInfo.ReleaseName,
		ReleaseURL:      versionInfo.ReleaseURL,
		Changelog:       versionInfo.Changelog,
		PublishedAt:     versionInfo.PublishedAt,
	}

	return result, nil
}

// fetchVersionInfo fetches version info from the API
func (m *Manager) fetchVersionInfo(ctx context.Context) (*VersionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, VersionAPIURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", fmt.Sprintf("FastCP/%s", m.currentVersion))

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var versionInfo VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &versionInfo, nil
}

// isNewerVersion compares versions (simple string comparison for now)
// Assumes semantic versioning: v1.0.0, v0.1.3, etc.
func (m *Manager) isNewerVersion(latest string) bool {
	current := strings.TrimPrefix(m.currentVersion, "v")
	latest = strings.TrimPrefix(latest, "v")

	// Simple version comparison
	currentParts := strings.Split(current, ".")
	latestParts := strings.Split(latest, ".")

	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return len(latestParts) > len(currentParts)
}

// GetUpgradeStatus returns the current upgrade status
func (m *Manager) GetUpgradeStatus() *UpgradeStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.status == nil {
		return &UpgradeStatus{}
	}
	return m.status
}

// IsUpgrading returns true if an upgrade is in progress
func (m *Manager) IsUpgrading() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status != nil && m.status.InProgress
}

// StartUpgrade begins the upgrade process asynchronously
func (m *Manager) StartUpgrade(ctx context.Context) error {
	m.mu.Lock()
	if m.status != nil && m.status.InProgress {
		m.mu.Unlock()
		return ErrUpgradeInProgress
	}

	m.status = &UpgradeStatus{
		InProgress: true,
		Message:    "Starting upgrade...",
		StartedAt:  time.Now(),
	}
	m.mu.Unlock()

	// Create lock file
	lockFile := filepath.Join(m.dataDir, ".upgrading")
	if err := os.WriteFile(lockFile, []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
		m.updateStatus("Failed to create lock file", false, err)
		return err
	}

	// Run upgrade in background
	go func() {
		err := m.performUpgrade(context.Background())

		// Remove lock file
		os.Remove(lockFile)

		m.mu.Lock()
		if err != nil {
			m.status = &UpgradeStatus{
				InProgress:  false,
				Success:     false,
				Error:       err.Error(),
				Message:     "Upgrade failed",
				CompletedAt: time.Now(),
			}
		} else {
			m.status = &UpgradeStatus{
				InProgress:  false,
				Success:     true,
				Message:     "Upgrade completed successfully. Restarting...",
				CompletedAt: time.Now(),
			}
		}
		m.mu.Unlock()

		// If successful, restart the service
		if err == nil {
			time.Sleep(2 * time.Second) // Give time for status to be read
			m.restartService()
		}
	}()

	return nil
}

// performUpgrade does the actual upgrade work
func (m *Manager) performUpgrade(ctx context.Context) error {
	// Step 1: Fetch version info
	m.updateStatus("Checking for updates...", true, nil)
	versionInfo, err := m.fetchVersionInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch version info: %w", err)
	}

	if !m.isNewerVersion(versionInfo.Version) {
		return ErrNoUpdateAvailable
	}

	// Step 2: Determine download URL based on architecture
	m.updateStatus("Determining architecture...", true, nil)
	downloadURL, err := m.getDownloadURL(versionInfo)
	if err != nil {
		return err
	}

	// Step 3: Download new binary
	m.updateStatus(fmt.Sprintf("Downloading %s...", versionInfo.Version), true, nil)
	tmpPath, err := m.downloadBinary(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer os.Remove(tmpPath)

	// Step 4: Backup current binary
	m.updateStatus("Backing up current binary...", true, nil)
	backupPath := BinaryPath + ".backup"
	if err := m.copyFile(BinaryPath, backupPath); err != nil {
		// Not fatal, continue anyway
		fmt.Printf("Warning: failed to backup current binary: %v\n", err)
	}

	// Step 5: Replace binary
	m.updateStatus("Installing new binary...", true, nil)
	if err := m.replaceBinary(tmpPath); err != nil {
		// Try to restore backup
		if _, statErr := os.Stat(backupPath); statErr == nil {
			os.Rename(backupPath, BinaryPath)
		}
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	// Clean up backup on success
	os.Remove(backupPath)

	m.updateStatus("Upgrade completed successfully!", true, nil)
	return nil
}

// getDownloadURL returns the appropriate download URL for the current architecture
func (m *Manager) getDownloadURL(versionInfo *VersionInfo) (string, error) {
	arch := runtime.GOARCH

	var key string
	switch arch {
	case "amd64":
		key = "linux_x86_64"
	case "arm64":
		key = "linux_aarch64"
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedArch, arch)
	}

	url, ok := versionInfo.Downloads[key]
	if !ok {
		return "", fmt.Errorf("no download available for %s", key)
	}

	return url, nil
}

// downloadBinary downloads the new binary to a temp file
func (m *Manager) downloadBinary(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", fmt.Sprintf("FastCP/%s", m.currentVersion))

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "fastcp-upgrade-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Copy with progress tracking
	written, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	if written == 0 {
		os.Remove(tmpFile.Name())
		return "", errors.New("downloaded file is empty")
	}

	// Make executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// replaceBinary replaces the current binary with the new one
func (m *Manager) replaceBinary(newBinaryPath string) error {
	// First try atomic rename
	if err := os.Rename(newBinaryPath, BinaryPath); err == nil {
		return nil
	}

	// If rename fails (cross-device), copy instead
	return m.copyFile(newBinaryPath, BinaryPath)
}

// copyFile copies a file from src to dst
func (m *Manager) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return os.Chmod(dst, 0755)
}

// restartService restarts the FastCP systemd service
func (m *Manager) restartService() {
	// Use systemctl to restart
	cmd := exec.Command("systemctl", "restart", "fastcp")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to restart service: %v\n", err)
		// Try alternative: just exit and let systemd restart us
		os.Exit(0)
	}
}

// updateStatus updates the upgrade status
func (m *Manager) updateStatus(message string, inProgress bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status == nil {
		m.status = &UpgradeStatus{}
	}

	m.status.Message = message
	m.status.InProgress = inProgress
	if err != nil {
		m.status.Error = err.Error()
		m.status.Success = false
	}
}

// CheckLockFile checks if an upgrade was interrupted (lock file exists)
func (m *Manager) CheckLockFile() bool {
	lockFile := filepath.Join(m.dataDir, ".upgrading")
	_, err := os.Stat(lockFile)
	return err == nil
}

// CleanupLockFile removes the upgrade lock file
func (m *Manager) CleanupLockFile() {
	lockFile := filepath.Join(m.dataDir, ".upgrading")
	os.Remove(lockFile)
}

