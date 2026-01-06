package php

import (
	"context"
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

	"github.com/rehmatworks/fastcp/internal/config"
)

// UserInstance represents a FrankenPHP instance running for a specific user and PHP version
type UserInstance struct {
	Username   string
	PHPVersion string
	SocketPath string // /home/username/run/php-8.3.sock
	PIDFile    string // /home/username/run/php-8.3.pid
	LogFile    string // /home/username/log/php-8.3.log
	Process    *os.Process
	Status     string
	StartedAt  time.Time
	SiteCount  int // Number of sites using this instance
}

// UserInstanceKey creates a unique key for user+version
func UserInstanceKey(username, version string) string {
	return fmt.Sprintf("%s:%s", username, version)
}

// UserPHPManager manages per-user PHP instances
type UserPHPManager struct {
	instances map[string]*UserInstance // key: "username:version"
	mu        sync.RWMutex
}

// NewUserPHPManager creates a new user PHP manager
func NewUserPHPManager() *UserPHPManager {
	return &UserPHPManager{
		instances: make(map[string]*UserInstance),
	}
}

// GetSocketPath returns the socket path for a user and PHP version
func GetSocketPath(username, version string) string {
	return filepath.Join("/home", username, "run", fmt.Sprintf("php-%s.sock", version))
}

// GetPIDPath returns the PID file path for a user and PHP version
func GetPIDPath(username, version string) string {
	return filepath.Join("/home", username, "run", fmt.Sprintf("php-%s.pid", version))
}

// GetLogPath returns the log file path for a user and PHP version
func GetLogPath(username, version string) string {
	return filepath.Join("/home", username, "log", fmt.Sprintf("php-%s.log", version))
}

// EnsureUserDirectories creates the run/ and log/ directories for a user
func EnsureUserDirectories(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	dirs := []string{
		filepath.Join("/home", username, "run"),
		filepath.Join("/home", username, "log"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("failed to set ownership on %s: %w", dir, err)
		}
	}

	return nil
}

// StartInstance starts a FrankenPHP instance for a user and PHP version
func (m *UserPHPManager) StartInstance(username, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := UserInstanceKey(username, version)

	// Check if already running
	if inst, exists := m.instances[key]; exists && inst.Status == "running" {
		inst.SiteCount++
		return nil
	}

	// Ensure user directories exist
	if err := EnsureUserDirectories(username); err != nil {
		return err
	}

	// Get user credentials
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Get PHP binary path
	cfg := config.Get()
	var binaryPath string
	for _, pv := range cfg.PHPVersions {
		if pv.Version == version && pv.Enabled {
			binaryPath = pv.BinaryPath
			break
		}
	}
	if binaryPath == "" {
		return fmt.Errorf("PHP version %s not configured or not enabled", version)
	}

	// Create instance
	inst := &UserInstance{
		Username:   username,
		PHPVersion: version,
		SocketPath: GetSocketPath(username, version),
		PIDFile:    GetPIDPath(username, version),
		LogFile:    GetLogPath(username, version),
		Status:     "starting",
		SiteCount:  1,
	}

	// Generate Caddyfile for this user instance
	caddyConfig := m.generateUserCaddyfile(username, version, inst.SocketPath)
	configPath := filepath.Join("/home", username, "run", fmt.Sprintf("Caddyfile.php-%s", version))
	if err := os.WriteFile(configPath, []byte(caddyConfig), 0644); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}
	_ = os.Chown(configPath, uid, gid)

	// Create log file
	logFile, err := os.OpenFile(inst.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	_ = os.Chown(inst.LogFile, uid, gid)

	// Start FrankenPHP process as the user
	cmd := exec.Command(binaryPath, "run", "--config", configPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if runtime.GOOS == "linux" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start FrankenPHP: %w", err)
	}

	// Save PID
	if err := os.WriteFile(inst.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		_ = cmd.Process.Kill()
		logFile.Close()
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	_ = os.Chown(inst.PIDFile, uid, gid)

	inst.Process = cmd.Process
	inst.Status = "running"
	inst.StartedAt = time.Now()

	m.instances[key] = inst

	fmt.Printf("[FastCP] Started PHP %s for user '%s' (socket: %s)\n", version, username, inst.SocketPath)

	// Wait for socket to be ready
	m.waitForSocket(inst.SocketPath, 10*time.Second)

	return nil
}

// StopInstance stops a FrankenPHP instance for a user and PHP version
func (m *UserPHPManager) StopInstance(username, version string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := UserInstanceKey(username, version)
	inst, exists := m.instances[key]
	if !exists || inst.Status != "running" {
		return nil
	}

	// Decrement site count unless force stopping
	if !force {
		inst.SiteCount--
		if inst.SiteCount > 0 {
			// Other sites still using this instance
			return nil
		}
	}

	return m.stopInstanceUnlocked(key)
}

// StopAllUserInstances stops all PHP instances for a user
func (m *UserPHPManager) StopAllUserInstances(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for key, inst := range m.instances {
		if inst.Username == username {
			if err := m.stopInstanceUnlocked(key); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping instances: %s", strings.Join(errs, "; "))
	}
	return nil
}

// stopInstanceUnlocked stops an instance (caller must hold lock)
func (m *UserPHPManager) stopInstanceUnlocked(key string) error {
	inst, exists := m.instances[key]
	if !exists || inst.Process == nil {
		delete(m.instances, key)
		return nil
	}

	// Send graceful shutdown signal
	if err := inst.Process.Signal(syscall.SIGTERM); err != nil {
		_ = inst.Process.Kill()
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		_, err := inst.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = inst.Process.Kill()
	}

	// Cleanup files
	_ = os.Remove(inst.PIDFile)
	_ = os.Remove(inst.SocketPath)

	fmt.Printf("[FastCP] Stopped PHP %s for user '%s'\n", inst.PHPVersion, inst.Username)

	delete(m.instances, key)
	return nil
}

// GetInstance returns information about a user's PHP instance
func (m *UserPHPManager) GetInstance(username, version string) *UserInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := UserInstanceKey(username, version)
	if inst, exists := m.instances[key]; exists {
		return inst
	}
	return nil
}

// GetUserInstances returns all instances for a user
func (m *UserPHPManager) GetUserInstances(username string) []*UserInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*UserInstance
	for _, inst := range m.instances {
		if inst.Username == username {
			result = append(result, inst)
		}
	}
	return result
}

// GetAllInstances returns all running instances
func (m *UserPHPManager) GetAllInstances() []*UserInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*UserInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result
}

// IsInstanceRunning checks if an instance is running for user+version
func (m *UserPHPManager) IsInstanceRunning(username, version string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := UserInstanceKey(username, version)
	if inst, exists := m.instances[key]; exists {
		return inst.Status == "running"
	}
	return false
}

// ReloadInstance reloads the configuration for a user's PHP instance
func (m *UserPHPManager) ReloadInstance(username, version string) error {
	m.mu.RLock()
	inst := m.instances[UserInstanceKey(username, version)]
	m.mu.RUnlock()

	if inst == nil || inst.Status != "running" {
		return fmt.Errorf("instance not running")
	}

	// Use Unix socket admin API
	adminSocketPath := filepath.Join("/home", username, "run", fmt.Sprintf("php-%s-admin.sock", version))

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", adminSocketPath)
			},
		},
		Timeout: 30 * time.Second,
	}

	// Regenerate config
	caddyConfig := m.generateUserCaddyfile(username, version, inst.SocketPath)

	req, err := http.NewRequest(http.MethodPost, "http://localhost/load", strings.NewReader(caddyConfig))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reload failed: %s", string(body))
	}

	return nil
}

// generateUserCaddyfile generates a Caddyfile for a user's PHP instance
func (m *UserPHPManager) generateUserCaddyfile(username, version, socketPath string) string {
	adminSocketPath := filepath.Join("/home", username, "run", fmt.Sprintf("php-%s-admin.sock", version))
	logPath := filepath.Join("/home", username, "log", fmt.Sprintf("php-%s-access.log", version))
	wwwDir := filepath.Join("/home", username, "www")

	return fmt.Sprintf(`# FrankenPHP instance for user: %s, PHP: %s
# Auto-generated - Do not edit manually

{
	# Admin API on Unix socket
	admin unix/%s
	
	# Disable automatic HTTPS for this internal server
	auto_https off
	
	log {
		output file %s {
			roll_size 50mb
			roll_keep 3
		}
		format json
	}
	
	# FrankenPHP specific settings
	frankenphp {
		num_threads 4
	}
}

# Listen on Unix socket
http:// {
	bind unix/%s

	root * %s
	
	# PHP handling
	php_server {
		resolve_root_symlink
	}
	
	# File server for static files
	file_server
	
	# Logging
	log
}
`, username, version, adminSocketPath, logPath, socketPath, wwwDir)
}

// waitForSocket waits for a Unix socket to become available
func (m *UserPHPManager) waitForSocket(socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// RecoverInstances tries to recover running instances from PID files
func (m *UserPHPManager) RecoverInstances() error {
	// Walk through /home/*/run/*.pid files
	matches, err := filepath.Glob("/home/*/run/php-*.pid")
	if err != nil {
		return err
	}

	for _, pidFile := range matches {
		// Parse username and version from path
		// /home/username/run/php-8.3.pid
		parts := strings.Split(pidFile, "/")
		if len(parts) < 5 {
			continue
		}
		username := parts[2]
		filename := parts[4]
		version := strings.TrimPrefix(strings.TrimSuffix(filename, ".pid"), "php-")

		// Read PID
		pidData, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			continue
		}

		// Check if process is running
		process, err := os.FindProcess(pid)
		if err != nil {
			_ = os.Remove(pidFile)
			continue
		}

		// On Linux, FindProcess always succeeds, so we need to check if it's actually running
		if err := process.Signal(syscall.Signal(0)); err != nil {
			_ = os.Remove(pidFile)
			continue
		}

		// Process is running, add to our tracking
		key := UserInstanceKey(username, version)
		m.instances[key] = &UserInstance{
			Username:   username,
			PHPVersion: version,
			SocketPath: GetSocketPath(username, version),
			PIDFile:    pidFile,
			LogFile:    GetLogPath(username, version),
			Process:    process,
			Status:     "running",
			StartedAt:  time.Now(), // We don't know the actual start time
			SiteCount:  1,
		}

		fmt.Printf("[FastCP] Recovered PHP %s instance for user '%s' (pid: %d)\n", version, username, pid)
	}

	return nil
}

// StopAll stops all running instances
func (m *UserPHPManager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for key := range m.instances {
		if err := m.stopInstanceUnlocked(key); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping instances: %s", strings.Join(errs, "; "))
	}
	return nil
}

// GetInstanceStatus returns status information for API responses
type InstanceStatus struct {
	Username   string    `json:"username"`
	PHPVersion string    `json:"php_version"`
	SocketPath string    `json:"socket_path"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	SiteCount  int       `json:"site_count"`
}

// GetStatus returns status of all instances
func (m *UserPHPManager) GetStatus() []InstanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]InstanceStatus, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, InstanceStatus{
			Username:   inst.Username,
			PHPVersion: inst.PHPVersion,
			SocketPath: inst.SocketPath,
			Status:     inst.Status,
			StartedAt:  inst.StartedAt,
			SiteCount:  inst.SiteCount,
		})
	}
	return result
}

