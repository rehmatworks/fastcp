package database

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

var (
	ErrDatabaseNotFound = errors.New("database not found")
	ErrDatabaseExists   = errors.New("database already exists")
	ErrUserExists       = errors.New("database user already exists")
	ErrMySQLNotInstalled = errors.New("MySQL is not installed")
	ErrMySQLNotRunning  = errors.New("MySQL is not running")
)

// InstallStatus represents the MySQL installation status
type InstallStatus struct {
	InProgress bool   `json:"in_progress"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	Message    string `json:"message,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
}

// Manager handles database operations
type Manager struct {
	databases     map[string]*models.Database
	mu            sync.RWMutex
	dataFile      string
	mysqlRootPwd  string
	installStatus *InstallStatus
	installMu     sync.RWMutex
}

// NewManager creates a new database manager
func NewManager() *Manager {
	cfg := config.Get()
	dataFile := filepath.Join(cfg.DataDir, "databases.json")

	m := &Manager{
		databases: make(map[string]*models.Database),
		dataFile:  dataFile,
	}

	m.load()
	return m
}

// load loads databases from disk
func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var databases []*models.Database
	if err := json.Unmarshal(data, &databases); err != nil {
		return err
	}

	for _, db := range databases {
		m.databases[db.ID] = db
	}

	return nil
}

// save saves databases to disk
func (m *Manager) save() error {
	databases := make([]*models.Database, 0, len(m.databases))
	for _, db := range m.databases {
		databases = append(databases, db)
	}

	data, err := json.MarshalIndent(databases, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.dataFile), 0755); err != nil {
		return err
	}

	return os.WriteFile(m.dataFile, data, 0644)
}

// IsMySQLInstalled checks if MySQL/MariaDB SERVER is installed
func (m *Manager) IsMySQLInstalled() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check for mysql client
	if _, err := exec.LookPath("mysql"); err != nil {
		return false
	}

	// Method 1: Check if mysqld process exists
	cmd := exec.Command("pgrep", "-x", "mysqld")
	if err := cmd.Run(); err == nil {
		return true // MySQL is running, so it's installed
	}

	// Method 2: Check if mysql service exists in systemd
	cmd = exec.Command("systemctl", "list-unit-files", "mysql.service")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "mysql.service") {
		return true
	}

	cmd = exec.Command("systemctl", "list-unit-files", "mariadb.service")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "mariadb.service") {
		return true
	}

	// Method 3: Check if mysql-server package is installed (dpkg shows 'ii' for installed)
	cmd = exec.Command("dpkg-query", "-W", "-f=${Status}", "mysql-server")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "install ok installed") {
		return true
	}

	// Try mariadb-server
	cmd = exec.Command("dpkg-query", "-W", "-f=${Status}", "mariadb-server")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "install ok installed") {
		return true
	}

	// Method 4: Check for mysqld binary
	if _, err := exec.LookPath("mysqld"); err == nil {
		return true
	}

	return false
}

// IsMySQLRunning checks if MySQL is running
func (m *Manager) IsMySQLRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "mysql")
	if err := cmd.Run(); err != nil {
		// Try mariadb
		cmd = exec.Command("systemctl", "is-active", "--quiet", "mariadb")
		return cmd.Run() == nil
	}
	return true
}

// InstallMySQL installs and secures MySQL
func (m *Manager) InstallMySQL() error {
	if runtime.GOOS != "linux" {
		return errors.New("MySQL installation only supported on Linux")
	}

	log := func(msg string) {
		fmt.Printf("[MySQL Install] %s\n", msg)
	}

	log("Starting MySQL installation...")

	// Update package list
	log("Running apt-get update...")
	cmd := exec.Command("apt-get", "update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update packages: %w", err)
	}
	log("Package list updated")

	// Install MySQL server (non-interactive)
	log("Installing mysql-server package (this may take a few minutes)...")
	cmd = exec.Command("apt-get", "install", "-y", "mysql-server")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install MySQL: %w", err)
	}
	log("MySQL package installed")

	// Configure MySQL for better performance
	log("Configuring MySQL performance settings...")
	if err := m.writePerformanceConfig(); err != nil {
		log(fmt.Sprintf("Warning: failed to write performance config: %v", err))
	}

	// Start MySQL
	log("Starting MySQL service...")
	cmd = exec.Command("systemctl", "start", "mysql")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try mariadb
		cmd = exec.Command("systemctl", "start", "mariadb")
		if output, err = cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start MySQL: %s - %s", err.Error(), string(output))
		}
	}
	log("MySQL service started")

	// Enable MySQL to start on boot
	log("Enabling MySQL on boot...")
	cmd = exec.Command("systemctl", "enable", "mysql")
	_ = cmd.Run()

	// Secure MySQL installation (optional - don't fail if it doesn't work)
	log("Securing MySQL installation...")
	if err := m.secureMySQLInstallation(); err != nil {
		log(fmt.Sprintf("Warning: Could not fully secure MySQL: %v", err))
		log("FastCP will use socket authentication instead.")
	}
	log("MySQL installation complete!")

	return nil
}

// AdoptMySQL configures FastCP to work with an existing MySQL installation
// This is called when MySQL is already installed but FastCP hasn't set it up
func (m *Manager) AdoptMySQL() error {
	if runtime.GOOS != "linux" {
		return errors.New("MySQL adoption only supported on Linux")
	}

	log := func(msg string) {
		fmt.Printf("[MySQL Adopt] %s\n", msg)
	}

	log("Adopting existing MySQL installation...")

	// Make sure MySQL is running
	if !m.IsMySQLRunning() {
		log("Starting MySQL service...")
		cmd := exec.Command("systemctl", "start", "mysql")
		if output, err := cmd.CombinedOutput(); err != nil {
			cmd = exec.Command("systemctl", "start", "mariadb")
			if output, err = cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to start MySQL: %s", string(output))
			}
		}
	}

	// Test if we can connect using socket auth (running as root)
	log("Testing MySQL connection...")
	if err := m.execMySQL("", "SELECT 1;"); err != nil {
		return fmt.Errorf("cannot connect to MySQL: %w", err)
	}

	log("MySQL connection successful!")
	log("FastCP will use socket authentication for database operations.")

	return nil
}

// GetInstallStatus returns the current MySQL installation status
func (m *Manager) GetInstallStatus() *InstallStatus {
	m.installMu.RLock()
	defer m.installMu.RUnlock()

	if m.installStatus == nil {
		return &InstallStatus{InProgress: false}
	}
	return m.installStatus
}

// IsInstalling returns true if MySQL installation is in progress
func (m *Manager) IsInstalling() bool {
	m.installMu.RLock()
	defer m.installMu.RUnlock()
	return m.installStatus != nil && m.installStatus.InProgress
}

// InstallMySQLAsync starts MySQL installation in the background
func (m *Manager) InstallMySQLAsync() error {
	m.installMu.Lock()
	defer m.installMu.Unlock()

	// Check if already installing
	if m.installStatus != nil && m.installStatus.InProgress {
		return errors.New("MySQL installation already in progress")
	}

	// Set initial status
	m.installStatus = &InstallStatus{
		InProgress: true,
		Message:    "Starting MySQL installation...",
		StartedAt:  time.Now(),
	}

	// Run installation in background
	go func() {
		var finalErr error

		// Check if MySQL is already installed - if so, just adopt it
		if m.IsMySQLInstalled() {
			m.updateInstallStatus("Adopting existing MySQL installation...", false, nil)
			finalErr = m.AdoptMySQL()
		} else {
			finalErr = m.InstallMySQL()
		}

		// Update final status
		m.installMu.Lock()
		if finalErr != nil {
			m.installStatus = &InstallStatus{
				InProgress: false,
				Success:    false,
				Error:      finalErr.Error(),
				Message:    "MySQL installation failed",
			}
		} else {
			m.installStatus = &InstallStatus{
				InProgress: false,
				Success:    true,
				Message:    "MySQL installed successfully",
			}
		}
		m.installMu.Unlock()
	}()

	return nil
}

// updateInstallStatus updates the installation status message
func (m *Manager) updateInstallStatus(message string, inProgress bool, err error) {
	m.installMu.Lock()
	defer m.installMu.Unlock()

	if m.installStatus == nil {
		m.installStatus = &InstallStatus{}
	}

	m.installStatus.Message = message
	m.installStatus.InProgress = inProgress
	if err != nil {
		m.installStatus.Error = err.Error()
		m.installStatus.Success = false
	}
}

// secureMySQLInstallation secures the MySQL installation
// On Ubuntu 22.04+ with MySQL 8.0, we use socket authentication for root
// which is more secure and reliable than password authentication
func (m *Manager) secureMySQLInstallation() error {
	// Security queries - we keep root using auth_socket (default on Ubuntu)
	// This is actually more secure as only the root OS user can access MySQL root
	queries := []string{
		// Remove anonymous users
		"DELETE FROM mysql.user WHERE User='';",
		// Remove remote root login
		"DELETE FROM mysql.user WHERE User='root' AND Host NOT IN ('localhost', '127.0.0.1', '::1');",
		// Remove test database
		"DROP DATABASE IF EXISTS test;",
		"DELETE FROM mysql.db WHERE Db='test' OR Db='test\\_%';",
		// Reload privileges
		"FLUSH PRIVILEGES;",
	}

	for _, query := range queries {
		// Use socket auth - FastCP runs as root so this works
		cmd := exec.Command("mysql", "-u", "root", "-e", query)
		if output, err := cmd.CombinedOutput(); err != nil {
			// Try with explicit socket path
			cmd = exec.Command("mysql", "-u", "root", "--socket=/var/run/mysqld/mysqld.sock", "-e", query)
			if output, err = cmd.CombinedOutput(); err != nil {
				// Non-fatal - continue with other queries
				fmt.Printf("[MySQL Secure] Warning: %s - %s\n", query[:min(50, len(query))], string(output))
			}
		}
	}

	// Mark that we're using socket auth (no password needed)
	m.mysqlRootPwd = ""

	return nil
}

// saveRootPassword saves the MySQL root password securely
func (m *Manager) saveRootPassword(password string) error {
	cfg := config.Get()
	pwdFile := filepath.Join(cfg.DataDir, ".mysql_root_pwd")

	// Create with restricted permissions
	if err := os.WriteFile(pwdFile, []byte(password), 0600); err != nil {
		return err
	}

	return nil
}

// getRootPassword retrieves the MySQL root password
func (m *Manager) getRootPassword() (string, error) {
	if m.mysqlRootPwd != "" {
		return m.mysqlRootPwd, nil
	}

	cfg := config.Get()
	pwdFile := filepath.Join(cfg.DataDir, ".mysql_root_pwd")

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		return "", err
	}

	m.mysqlRootPwd = strings.TrimSpace(string(data))
	return m.mysqlRootPwd, nil
}

// EnsureMySQL ensures MySQL is installed and running
func (m *Manager) EnsureMySQL() error {
	if !m.IsMySQLInstalled() {
		if err := m.InstallMySQL(); err != nil {
			return err
		}
	}

	if !m.IsMySQLRunning() {
		cmd := exec.Command("systemctl", "start", "mysql")
		if output, err := cmd.CombinedOutput(); err != nil {
			// Try mariadb
			cmd = exec.Command("systemctl", "start", "mariadb")
			if output, err = cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to start MySQL: %s", string(output))
			}
		}
	}

	return nil
}

// Create creates a new database and user
func (m *Manager) Create(db *models.Database) (*models.Database, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure MySQL is installed and running
	if err := m.EnsureMySQL(); err != nil {
		return nil, err
	}

	// Check if database already exists
	for _, existing := range m.databases {
		if existing.Name == db.Name {
			return nil, ErrDatabaseExists
		}
	}

	// Generate ID and timestamps
	db.ID = generateID()
	db.CreatedAt = time.Now()
	db.UpdatedAt = time.Now()

	// Generate password if not provided
	if db.Password == "" {
		db.Password = generatePassword(24)
	}

	// Get root password
	rootPwd, err := m.getRootPassword()
	if err != nil {
		// Try without password (socket auth)
		rootPwd = ""
	}

	// Create database
	if err := m.execMySQL(rootPwd, fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", db.Name)); err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Create user for both localhost and 127.0.0.1 to handle both socket and TCP connections
	// Using 127.0.0.1 since we force TCP connections in wp-config
	if err := m.execMySQL(rootPwd, fmt.Sprintf("CREATE USER '%s'@'127.0.0.1' IDENTIFIED BY '%s';", db.Username, db.Password)); err != nil {
		// Rollback: drop database
		m.execMySQL(rootPwd, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db.Name))
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Also create for localhost to handle socket connections
	_ = m.execMySQL(rootPwd, fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", db.Username, db.Password))

	// Grant privileges for both hosts
	if err := m.execMySQL(rootPwd, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'127.0.0.1';", db.Name, db.Username)); err != nil {
		// Rollback
		m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'127.0.0.1';", db.Username))
		m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", db.Username))
		m.execMySQL(rootPwd, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db.Name))
		return nil, fmt.Errorf("failed to grant privileges: %w", err)
	}

	// Grant for localhost too
	_ = m.execMySQL(rootPwd, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", db.Name, db.Username))

	// Flush privileges
	m.execMySQL(rootPwd, "FLUSH PRIVILEGES;")

	// Save to storage
	m.databases[db.ID] = db
	if err := m.save(); err != nil {
		return nil, err
	}

	return db, nil
}

// Get returns a database by ID
func (m *Manager) Get(id string) (*models.Database, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	db, ok := m.databases[id]
	if !ok {
		return nil, ErrDatabaseNotFound
	}

	return db, nil
}

// List returns all databases, optionally filtered by user
func (m *Manager) List(userID string) []*models.Database {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*models.Database
	for _, db := range m.databases {
		if userID == "" || db.UserID == userID {
			result = append(result, db)
		}
	}

	return result
}

// Delete deletes a database and its user
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	db, ok := m.databases[id]
	if !ok {
		return ErrDatabaseNotFound
	}

	rootPwd, _ := m.getRootPassword()

	// Drop users from both hosts
	m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", db.Username))
	m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'127.0.0.1';", db.Username))

	// Drop database
	if err := m.execMySQL(rootPwd, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db.Name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Flush privileges
	m.execMySQL(rootPwd, "FLUSH PRIVILEGES;")

	// Remove from storage
	delete(m.databases, id)
	return m.save()
}

// UpdatePassword updates a database user's password
func (m *Manager) UpdatePassword(id, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	db, ok := m.databases[id]
	if !ok {
		return ErrDatabaseNotFound
	}

	rootPwd, _ := m.getRootPassword()

	// Update password for both hosts
	if err := m.execMySQL(rootPwd, fmt.Sprintf("ALTER USER '%s'@'127.0.0.1' IDENTIFIED BY '%s';", db.Username, newPassword)); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	// Also update localhost user if it exists
	_ = m.execMySQL(rootPwd, fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", db.Username, newPassword))

	m.execMySQL(rootPwd, "FLUSH PRIVILEGES;")

	db.Password = newPassword
	db.UpdatedAt = time.Now()

	return m.save()
}

// GetStatus returns MySQL server status
func (m *Manager) GetStatus() *models.DatabaseServerStatus {
	status := &models.DatabaseServerStatus{
		Installed: m.IsMySQLInstalled(),
		Running:   m.IsMySQLRunning(),
	}

	if status.Running {
		// Get version
		cmd := exec.Command("mysql", "-V")
		if output, err := cmd.Output(); err == nil {
			status.Version = strings.TrimSpace(string(output))
		}

		// Get database count
		status.DatabaseCount = len(m.databases)
	}

	return status
}

// writePerformanceConfig writes MySQL performance configuration
func (m *Manager) writePerformanceConfig() error {
	configContent := `# FastCP MySQL Performance Configuration
[mysqld]
# Disable performance schema for better performance on small servers
performance_schema = OFF

# Reduce memory usage
innodb_buffer_pool_size = 128M
innodb_log_buffer_size = 8M
max_connections = 100

# Improve query performance
query_cache_type = 1
query_cache_size = 16M
query_cache_limit = 1M

# Logging
slow_query_log = 1
slow_query_log_file = /var/log/mysql/slow.log
long_query_time = 2
`

	configPath := "/etc/mysql/conf.d/fastcp.cnf"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		// Try alternative path
		configPath = "/etc/mysql/mysql.conf.d/fastcp.cnf"
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	return nil
}

// execMySQL executes a MySQL query as root
// It tries password auth first (if password provided), then falls back to socket auth
func (m *Manager) execMySQL(rootPwd, query string) error {
	var cmd *exec.Cmd
	var output []byte
	var err error

	// Try with password first if available
	if rootPwd != "" {
		cmd = exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", rootPwd), "-e", query)
		output, err = cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		// Password auth failed, fall through to socket auth
	}

	// Try socket auth (works on Ubuntu 22.04+ when running as root)
	// This uses auth_socket plugin which authenticates based on the OS user
	cmd = exec.Command("mysql", "-u", "root", "-e", query)
	output, err = cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	// Last resort: try with explicit socket path
	cmd = exec.Command("mysql", "-u", "root", "--socket=/var/run/mysqld/mysqld.sock", "-e", query)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err.Error(), string(output))
	}

	return nil
}

// generateID generates a unique ID
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generatePassword generates a random password
func generatePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

