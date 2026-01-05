package php

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rehmatworks/fastcp/internal/caddy"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/downloader"
	"github.com/rehmatworks/fastcp/internal/models"
)

const (
	// PHPUser is the dedicated user for running FrankenPHP
	PHPUser  = "fastcp"
	PHPGroup = "fastcp"
)

// Manager manages PHP FrankenPHP instances and the main proxy
type Manager struct {
	instances    map[string]*Instance
	proxy        *ProxyInstance
	mu           sync.RWMutex
	generator    *caddy.Generator
	sitesFunc    func() []models.Site // Function to get current sites
}

// Instance represents a running FrankenPHP instance
type Instance struct {
	Config    models.PHPVersionConfig
	Process   *os.Process
	Status    string
	StartedAt time.Time
	PIDFile   string
}

// ProxyInstance represents the main Caddy reverse proxy
type ProxyInstance struct {
	Process   *os.Process
	Status    string
	StartedAt time.Time
	PIDFile   string
	HTTPPort  int
	HTTPSPort int
}

// EnsurePHPUser creates the fastcp system user if it doesn't exist
// This user is used to run FrankenPHP with reduced privileges
func EnsurePHPUser() error {
	if runtime.GOOS != "linux" {
		return nil // Only needed on Linux
	}

	// Check if user already exists
	if _, err := user.Lookup(PHPUser); err == nil {
		fmt.Printf("[FastCP] User '%s' already exists\n", PHPUser)
		return nil // User exists
	}

	fmt.Printf("[FastCP] Creating system user '%s' for PHP...\n", PHPUser)

	// Check if group already exists
	groupExists := false
	if output, err := exec.Command("getent", "group", PHPGroup).Output(); err == nil && len(output) > 0 {
		groupExists = true
	}

	var cmd *exec.Cmd
	if groupExists {
		// Group exists, create user and add to existing group
		cmd = exec.Command("useradd",
			"--system",                       // System account
			"--no-create-home",               // No home directory
			"--shell", "/usr/sbin/nologin",   // No login
			"-g", PHPGroup,                   // Use existing group
			PHPUser,
		)
	} else {
		// Create user with matching group
		cmd = exec.Command("useradd",
			"--system",                       // System account
			"--no-create-home",               // No home directory
			"--shell", "/usr/sbin/nologin",   // No login
			"--user-group",                   // Create matching group
			PHPUser,
		)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if user was created despite error (race condition)
		if _, lookupErr := user.Lookup(PHPUser); lookupErr == nil {
			fmt.Printf("[FastCP] User '%s' already exists (concurrent creation)\n", PHPUser)
			return nil
		}
		return fmt.Errorf("failed to create user %s: %s - %s", PHPUser, err.Error(), string(output))
	}

	// Add fastcp user to www-data group for web file access
	cmd = exec.Command("usermod", "-a", "-G", "www-data", PHPUser)
	_ = cmd.Run() // Ignore error if www-data doesn't exist

	fmt.Printf("[FastCP] User '%s' created successfully\n", PHPUser)
	return nil
}

// GetPHPUserCredentials returns the UID and GID of the fastcp user
func GetPHPUserCredentials() (uid, gid uint32, err error) {
	u, err := user.Lookup(PHPUser)
	if err != nil {
		return 0, 0, err
	}

	uidInt, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}

	gidInt, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}

	return uint32(uidInt), uint32(gidInt), nil
}

// NewManager creates a new PHP instance manager
func NewManager(generator *caddy.Generator, sitesFunc func() []models.Site) *Manager {
	return &Manager{
		instances: make(map[string]*Instance),
		generator: generator,
		sitesFunc: sitesFunc,
	}
}

// Initialize initializes all configured PHP instances and the proxy
func (m *Manager) Initialize() error {
	cfg := config.Get()

	// Initialize PHP instances
	for _, pv := range cfg.PHPVersions {
		if pv.Enabled {
			m.instances[pv.Version] = &Instance{
				Config:  pv,
				Status:  "stopped",
				PIDFile: filepath.Join(cfg.DataDir, "run", fmt.Sprintf("php-%s.pid", pv.Version)),
			}
		}
	}

	// Initialize proxy
	m.proxy = &ProxyInstance{
		Status:    "stopped",
		PIDFile:   filepath.Join(cfg.DataDir, "run", "proxy.pid"),
		HTTPPort:  cfg.ProxyPort,
		HTTPSPort: cfg.ProxySSLPort,
	}

	return nil
}

// EnsureBinaries checks if PHP binaries exist and downloads them if missing
func (m *Manager) EnsureBinaries(ctx context.Context, logger interface{ Info(msg string, args ...any) }) error {
	cfg := config.Get()

	for _, pv := range cfg.PHPVersions {
		if !pv.Enabled {
			continue
		}

		// Check if binary exists
		if _, err := os.Stat(pv.BinaryPath); err == nil {
			logger.Info("PHP binary found", "version", pv.Version, "path", pv.BinaryPath)
			continue
		}

		// Binary doesn't exist - download it
		logger.Info("PHP binary not found, downloading...", "version", pv.Version, "path", pv.BinaryPath)

		dm := downloader.NewManager(downloader.Config{
			Source:   downloader.SourceGitHub,
			CacheDir: filepath.Join(cfg.DataDir, "downloads"),
		})

		err := dm.InstallPHPVersion(ctx, pv.Version, pv.BinaryPath, func(progress downloader.DownloadProgress) {
			if int(progress.Percent)%10 == 0 {
				logger.Info("Downloading PHP",
					"version", pv.Version,
					"progress", fmt.Sprintf("%.1f%%", progress.Percent),
					"downloaded", fmt.Sprintf("%dMB", progress.Downloaded/1024/1024),
				)
			}
		})

		if err != nil {
			return fmt.Errorf("failed to download PHP %s: %w", pv.Version, err)
		}

		logger.Info("PHP binary downloaded successfully", "version", pv.Version, "path", pv.BinaryPath)
	}

	return nil
}

// StartAll starts all enabled PHP instances and the proxy
func (m *Manager) StartAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start PHP instances first
	for version := range m.instances {
		if err := m.startInstance(version); err != nil {
			return fmt.Errorf("failed to start PHP %s: %w", version, err)
		}
	}

	// Start the main proxy
	if err := m.startProxy(); err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}

	return nil
}

// StopAll stops all running PHP instances and the proxy
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop proxy first
	if err := m.stopProxy(); err != nil {
		return fmt.Errorf("failed to stop proxy: %w", err)
	}

	// Stop PHP instances
	for version := range m.instances {
		if err := m.stopInstance(version); err != nil {
			return fmt.Errorf("failed to stop PHP %s: %w", version, err)
		}
	}

	return nil
}

// Start starts a specific PHP instance
func (m *Manager) Start(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startInstance(version)
}

// Stop stops a specific PHP instance
func (m *Manager) Stop(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopInstance(version)
}

// Restart restarts a specific PHP instance
func (m *Manager) Restart(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.stopInstance(version); err != nil {
		return err
	}

	// Wait a moment for cleanup
	time.Sleep(500 * time.Millisecond)

	return m.startInstance(version)
}

// RestartWorkers restarts workers for a specific PHP instance via admin API
func (m *Manager) RestartWorkers(version string) error {
	m.mu.RLock()
	instance, ok := m.instances[version]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("PHP version %s not found", version)
	}

	if instance.Status != "running" {
		return fmt.Errorf("PHP %s is not running", version)
	}

	// Call the FrankenPHP admin API to restart workers
	url := fmt.Sprintf("http://localhost:%d/frankenphp/workers/restart", instance.Config.AdminPort)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to restart workers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to restart workers: %s", string(body))
	}

	return nil
}

// Reload reloads configuration for all instances and the proxy
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sites := m.sitesFunc()

	// Reload PHP instances
	for version, instance := range m.instances {
		if instance.Status == "running" {
			// Regenerate Caddyfile for this version
			content, err := m.generator.GeneratePHPInstance(
				version,
				instance.Config.Port,
				instance.Config.AdminPort,
				sites,
			)
			if err != nil {
				return fmt.Errorf("failed to generate config for PHP %s: %w", version, err)
			}

			if err := m.generator.WritePHPInstance(version, content); err != nil {
				return fmt.Errorf("failed to write config for PHP %s: %w", version, err)
			}

			// Reload via Caddy admin API
			if err := m.reloadInstance(version); err != nil {
				return fmt.Errorf("failed to reload PHP %s: %w", version, err)
			}
		}
	}

	// Reload proxy
	if err := m.reloadProxy(); err != nil {
		return fmt.Errorf("failed to reload proxy: %w", err)
	}

	return nil
}

// GetStatus returns the status of all PHP instances
func (m *Manager) GetStatus() []models.PHPInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sites := m.sitesFunc()
	siteCounts := make(map[string]int)
	for _, site := range sites {
		if site.Status == "active" {
			siteCounts[site.PHPVersion]++
		}
	}

	var result []models.PHPInstance
	for version, instance := range m.instances {
		info := models.PHPInstance{
			Version:    version,
			Port:       instance.Config.Port,
			AdminPort:  instance.Config.AdminPort,
			BinaryPath: instance.Config.BinaryPath,
			Status:     instance.Status,
			SiteCount:  siteCounts[version],
		}

		if instance.Status == "running" {
			info.StartedAt = instance.StartedAt

			// Try to get thread info from admin API
			if threadInfo, err := m.getThreadInfo(instance); err == nil {
				info.ThreadCount = threadInfo.ThreadCount
				info.MaxThreads = threadInfo.MaxThreads
			}
		}

		result = append(result, info)
	}

	return result
}

// GetInstance returns information about a specific PHP instance
func (m *Manager) GetInstance(version string) (*models.PHPInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, ok := m.instances[version]
	if !ok {
		return nil, fmt.Errorf("PHP version %s not found", version)
	}

	sites := m.sitesFunc()
	siteCount := 0
	for _, site := range sites {
		if site.PHPVersion == version && site.Status == "active" {
			siteCount++
		}
	}

	info := &models.PHPInstance{
		Version:    version,
		Port:       instance.Config.Port,
		AdminPort:  instance.Config.AdminPort,
		BinaryPath: instance.Config.BinaryPath,
		Status:     instance.Status,
		SiteCount:  siteCount,
		StartedAt:  instance.StartedAt,
	}

	if instance.Status == "running" {
		if threadInfo, err := m.getThreadInfo(instance); err == nil {
			info.ThreadCount = threadInfo.ThreadCount
			info.MaxThreads = threadInfo.MaxThreads
		}
	}

	return info, nil
}

// startInstance starts a PHP instance (must hold lock)
func (m *Manager) startInstance(version string) error {
	instance, ok := m.instances[version]
	if !ok {
		return fmt.Errorf("PHP version %s not configured", version)
	}

	if instance.Status == "running" {
		return nil // Already running
	}

	cfg := config.Get()
	sites := m.sitesFunc()

	// Generate Caddyfile for this version
	content, err := m.generator.GeneratePHPInstance(
		version,
		instance.Config.Port,
		instance.Config.AdminPort,
		sites,
	)
	if err != nil {
		return err
	}

	configPath := filepath.Join(cfg.DataDir, "caddy", fmt.Sprintf("Caddyfile.php-%s", version))
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return err
	}

	// Create log directories with proper permissions for fastcp user
	logDir := filepath.Join(cfg.LogDir, fmt.Sprintf("php-%s", version))
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// Create socket directory with proper permissions for fastcp user
	socketDir := "/var/run/fastcp"
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return err
	}

	// Set ownership of directories to fastcp user
	if runtime.GOOS == "linux" {
		if uid, gid, err := GetPHPUserCredentials(); err == nil {
			// Own the main log directory
			os.Chown(cfg.LogDir, int(uid), int(gid))
			os.Chown(logDir, int(uid), int(gid))
			os.Chown(configPath, int(uid), int(gid))
			os.Chown(socketDir, int(uid), int(gid))
			
			// Create and own the PHP log file
			phpLogFile := filepath.Join(cfg.LogDir, fmt.Sprintf("php-%s.log", version))
			if f, err := os.OpenFile(phpLogFile, os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.Close()
				os.Chown(phpLogFile, int(uid), int(gid))
			}
		}
	}

	// Start FrankenPHP process
	cmd := exec.Command(instance.Config.BinaryPath, "run", "--config", configPath)
	
	// Log output to file for debugging
	logFile := filepath.Join(logDir, "frankenphp.log")
	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
		// Set ownership so fastcp user can write
		if runtime.GOOS == "linux" {
			if uid, gid, err := GetPHPUserCredentials(); err == nil {
				os.Chown(logFile, int(uid), int(gid))
			}
		}
	}

	// Run as fastcp user on Linux for security (if enabled)
	// Note: Requires `setcap 'cap_net_bind_service=+ep' /usr/local/bin/frankenphp`
	runAsFastCPUser := os.Getenv("FASTCP_PHP_USER") != "root"
	
	if runtime.GOOS == "linux" && runAsFastCPUser {
		if uid, gid, err := GetPHPUserCredentials(); err == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
				Credential: &syscall.Credential{
					Uid: uid,
					Gid: gid,
				},
			}
			fmt.Printf("[FastCP] Starting PHP %s as user 'fastcp' (uid=%d)\n", version, uid)
		} else {
			// Fallback: just setpgid if user not found
			fmt.Printf("[Warning] fastcp user not found, running PHP as current user: %v\n", err)
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
			}
		}
	} else {
		if !runAsFastCPUser {
			fmt.Printf("[FastCP] Starting PHP %s as root (FASTCP_PHP_USER=root)\n", version)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FrankenPHP: %w", err)
	}

	// Save PID
	pidDir := filepath.Dir(instance.PIDFile)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(instance.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		return err
	}

	instance.Process = cmd.Process
	instance.Status = "running"
	instance.StartedAt = time.Now()

	return nil
}

// stopInstance stops a PHP instance (must hold lock)
func (m *Manager) stopInstance(version string) error {
	instance, ok := m.instances[version]
	if !ok {
		return fmt.Errorf("PHP version %s not configured", version)
	}

	if instance.Status != "running" || instance.Process == nil {
		instance.Status = "stopped"
		return nil
	}

	// Send graceful shutdown signal
	if err := instance.Process.Signal(syscall.SIGTERM); err != nil {
		// If SIGTERM fails, try SIGKILL
		_ = instance.Process.Kill()
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		_, err := instance.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = instance.Process.Kill()
	}

	// Remove PID file
	_ = os.Remove(instance.PIDFile)

	instance.Process = nil
	instance.Status = "stopped"
	instance.StartedAt = time.Time{}

	return nil
}

// reloadInstance reloads a PHP instance via Caddy admin API
func (m *Manager) reloadInstance(version string) error {
	_, ok := m.instances[version]
	if !ok {
		return fmt.Errorf("PHP version %s not found", version)
	}

	cfg := config.Get()
	configPath := filepath.Join(cfg.DataDir, "caddy", fmt.Sprintf("Caddyfile.php-%s", version))

	// Read the new config
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// Use Unix socket for admin API
	adminSocketPath := fmt.Sprintf("/var/run/fastcp/php-%s-admin.sock", version)
	
	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", adminSocketPath)
			},
		},
		Timeout: 30 * time.Second,
	}

	// The URL host doesn't matter when using Unix socket, but path does
	req, err := http.NewRequest(http.MethodPost, "http://localhost/load", strings.NewReader(string(configData)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to admin socket %s: %w", adminSocketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reload: %s", string(body))
	}

	return nil
}

// startProxy starts the main reverse proxy (must hold lock)
func (m *Manager) startProxy() error {
	if m.proxy == nil {
		return fmt.Errorf("proxy not initialized")
	}

	if m.proxy.Status == "running" {
		return nil // Already running
	}

	cfg := config.Get()
	sites := m.sitesFunc()

	// Generate proxy Caddyfile
	content, err := m.generator.GenerateMainProxy(sites, cfg.PHPVersions, m.proxy.HTTPPort, m.proxy.HTTPSPort)
	if err != nil {
		return fmt.Errorf("failed to generate proxy config: %w", err)
	}

	configPath := filepath.Join(cfg.DataDir, "caddy", "Caddyfile.proxy")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return err
	}

	// Use the first available FrankenPHP binary as the proxy (it's just Caddy when not using frankenphp directive)
	var binaryPath string
	for _, pv := range cfg.PHPVersions {
		if pv.Enabled && pv.BinaryPath != "" {
			binaryPath = pv.BinaryPath
			break
		}
	}
	if binaryPath == "" {
		return fmt.Errorf("no FrankenPHP binary configured")
	}

	// Start proxy process
	cmd := exec.Command(binaryPath, "run", "--config", configPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}

	// Save PID
	pidDir := filepath.Dir(m.proxy.PIDFile)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(m.proxy.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		return err
	}

	m.proxy.Process = cmd.Process
	m.proxy.Status = "running"
	m.proxy.StartedAt = time.Now()

	return nil
}

// stopProxy stops the main reverse proxy (must hold lock)
func (m *Manager) stopProxy() error {
	if m.proxy == nil || m.proxy.Status != "running" || m.proxy.Process == nil {
		if m.proxy != nil {
			m.proxy.Status = "stopped"
		}
		return nil
	}

	// Send graceful shutdown signal
	if err := m.proxy.Process.Signal(syscall.SIGTERM); err != nil {
		_ = m.proxy.Process.Kill()
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		_, err := m.proxy.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = m.proxy.Process.Kill()
	}

	// Remove PID file
	_ = os.Remove(m.proxy.PIDFile)

	m.proxy.Process = nil
	m.proxy.Status = "stopped"
	m.proxy.StartedAt = time.Time{}

	return nil
}

// reloadProxy reloads the proxy configuration
func (m *Manager) reloadProxy() error {
	if m.proxy == nil || m.proxy.Status != "running" {
		return nil
	}

	cfg := config.Get()
	sites := m.sitesFunc()

	// Regenerate proxy config
	content, err := m.generator.GenerateMainProxy(sites, cfg.PHPVersions, m.proxy.HTTPPort, m.proxy.HTTPSPort)
	if err != nil {
		return err
	}

	configPath := filepath.Join(cfg.DataDir, "caddy", "Caddyfile.proxy")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return err
	}

	// Call Caddy admin API to reload
	url := "http://localhost:2019/load"
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reload proxy: %s", string(body))
	}

	return nil
}

// threadInfo holds thread information from admin API
type threadInfo struct {
	ThreadCount int
	MaxThreads  int
}

// getThreadInfo gets thread information from the admin API
func (m *Manager) getThreadInfo(instance *Instance) (*threadInfo, error) {
	url := fmt.Sprintf("http://localhost:%d/frankenphp/threads", instance.Config.AdminPort)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get thread info")
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	info := &threadInfo{}
	if threads, ok := data["Threads"].([]interface{}); ok {
		info.ThreadCount = len(threads)
	}

	return info, nil
}

