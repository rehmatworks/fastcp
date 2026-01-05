package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Source represents where to download FrankenPHP from
type Source string

const (
	SourceGitHub Source = "github"
	SourceCDN    Source = "cdn"
)

// Platform represents OS and architecture combination
type Platform struct {
	OS   string // "linux", "darwin"
	Arch string // "amd64", "arm64"
}

// PHPBinary represents a downloadable FrankenPHP binary
type PHPBinary struct {
	PHPVersion     string `json:"php_version"`      // "8.4", "8.3", "8.2"
	FrankenVersion string `json:"franken_version"`  // "1.11.1"
	Platform       string `json:"platform"`         // "linux-x86_64", "darwin-arm64"
	URL            string `json:"url"`              // Download URL
	Checksum       string `json:"checksum"`         // SHA256 checksum (optional)
	Size           int64  `json:"size"`             // File size in bytes
}

// DownloadProgress reports download progress
type DownloadProgress struct {
	Downloaded int64
	Total      int64
	Percent    float64
}

// ProgressCallback is called during download with progress updates
type ProgressCallback func(progress DownloadProgress)

// Manager handles downloading and managing FrankenPHP binaries
type Manager struct {
	source     Source
	cdnBaseURL string
	cacheDir   string
	httpClient *http.Client
}

// Config for the download manager
type Config struct {
	Source     Source // "github" or "cdn"
	CDNBaseURL string // Base URL for CDN (e.g., "https://cdn.fastcp.io/frankenphp")
	CacheDir   string // Directory to cache downloads
}

// NewManager creates a new download manager
func NewManager(cfg Config) *Manager {
	if cfg.Source == "" {
		cfg.Source = SourceGitHub
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(os.TempDir(), "fastcp", "downloads")
	}

	return &Manager{
		source:     cfg.Source,
		cdnBaseURL: cfg.CDNBaseURL,
		cacheDir:   cfg.CacheDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Minute, // Large timeout for big downloads
		},
	}
}

// DetectPlatform detects the current platform
func DetectPlatform() Platform {
	os := runtime.GOOS
	arch := runtime.GOARCH

	return Platform{
		OS:   os,
		Arch: arch,
	}
}

// PlatformString returns the FrankenPHP platform string
func (p Platform) String() string {
	osName := p.OS
	if osName == "darwin" {
		osName = "mac"
	}

	archName := p.Arch
	switch archName {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "arm64"
		if p.OS == "linux" {
			archName = "aarch64"
		}
	}

	return fmt.Sprintf("%s-%s", osName, archName)
}

// GetAvailableVersions returns available PHP versions
func (m *Manager) GetAvailableVersions(ctx context.Context) ([]PHPBinary, error) {
	switch m.source {
	case SourceGitHub:
		return m.getGitHubVersions(ctx)
	case SourceCDN:
		return m.getCDNVersions(ctx)
	default:
		return nil, fmt.Errorf("unknown source: %s", m.source)
	}
}

// getGitHubVersions fetches available versions from GitHub releases
func (m *Manager) getGitHubVersions(ctx context.Context) ([]PHPBinary, error) {
	platform := DetectPlatform()

	// Get latest release info from GitHub API
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/php/frankenphp/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release: %w", err)
	}

	// Find matching binary for current platform
	var binaries []PHPBinary
	platformStr := platform.String()

	for _, asset := range release.Assets {
		// Match platform (e.g., "frankenphp-linux-x86_64")
		if strings.Contains(asset.Name, platformStr) && !strings.Contains(asset.Name, "debug") {
			// Extract version (remove 'v' prefix)
			frankenVer := strings.TrimPrefix(release.TagName, "v")

			binaries = append(binaries, PHPBinary{
				PHPVersion:     "8.4", // Current GitHub releases are PHP 8.4
				FrankenVersion: frankenVer,
				Platform:       platformStr,
				URL:            asset.BrowserDownloadURL,
				Size:           asset.Size,
			})
		}
	}

	if len(binaries) == 0 {
		return nil, fmt.Errorf("no binary found for platform %s", platformStr)
	}

	return binaries, nil
}

// getCDNVersions fetches available versions from FastCP CDN
// This will be implemented when CDN is set up
func (m *Manager) getCDNVersions(ctx context.Context) ([]PHPBinary, error) {
	if m.cdnBaseURL == "" {
		return nil, fmt.Errorf("CDN base URL not configured")
	}

	// TODO: Implement CDN version fetching
	// Expected format: GET {cdnBaseURL}/versions.json
	// Returns: [{"php_version": "8.4", "franken_version": "1.11.1", "platform": "linux-x86_64", "url": "...", "checksum": "..."}, ...]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.cdnBaseURL+"/versions.json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CDN versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CDN returned status %d", resp.StatusCode)
	}

	var versions []PHPBinary
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("failed to parse CDN versions: %w", err)
	}

	// Filter for current platform
	platform := DetectPlatform()
	var filtered []PHPBinary
	for _, v := range versions {
		if v.Platform == platform.String() {
			filtered = append(filtered, v)
		}
	}

	return filtered, nil
}

// Download downloads a FrankenPHP binary
func (m *Manager) Download(ctx context.Context, binary PHPBinary, destPath string, progress ProgressCallback) error {
	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temporary file for download
	tmpFile, err := os.CreateTemp(destDir, "frankenphp-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath) // Clean up on error
	}()

	// Download the file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, binary.URL, nil)
	if err != nil {
		return err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Setup progress tracking
	total := resp.ContentLength
	if binary.Size > 0 {
		total = binary.Size
	}

	var downloaded int64
	buf := make([]byte, 32*1024) // 32KB buffer
	hasher := sha256.New()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// Write to file
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write failed: %w", writeErr)
			}

			// Update hash
			hasher.Write(buf[:n])

			// Update progress
			downloaded += int64(n)
			if progress != nil {
				percent := float64(0)
				if total > 0 {
					percent = float64(downloaded) / float64(total) * 100
				}
				progress(DownloadProgress{
					Downloaded: downloaded,
					Total:      total,
					Percent:    percent,
				})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}
	}

	// Verify checksum if provided
	if binary.Checksum != "" {
		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if actualChecksum != binary.Checksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", binary.Checksum, actualChecksum)
		}
	}

	// Close temp file before rename
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Move to final destination
	if err := os.Rename(tmpPath, destPath); err != nil {
		// If rename fails (cross-device), try copy
		return m.copyFile(tmpPath, destPath)
	}

	return nil
}

// copyFile copies a file (used when rename fails across devices)
func (m *Manager) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Set executable permission
	return os.Chmod(dst, 0755)
}

// GetInstalledVersion checks if a binary is already installed and returns its version
func (m *Manager) GetInstalledVersion(binaryPath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", nil
	}

	// Try to run frankenphp version
	// This would require executing the binary, which we'll skip for now
	// Just return that it exists
	return "installed", nil
}

// InstallPHPVersion downloads and installs a specific PHP version
func (m *Manager) InstallPHPVersion(ctx context.Context, phpVersion string, destPath string, progress ProgressCallback) error {
	// Get available versions
	versions, err := m.GetAvailableVersions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get available versions: %w", err)
	}

	// Find matching version
	var binary *PHPBinary
	for _, v := range versions {
		if v.PHPVersion == phpVersion {
			binary = &v
			break
		}
	}

	if binary == nil {
		// If exact version not found, use the latest available
		if len(versions) > 0 {
			binary = &versions[0]
		} else {
			return fmt.Errorf("no PHP version %s available for this platform", phpVersion)
		}
	}

	// Download
	return m.Download(ctx, *binary, destPath, progress)
}

