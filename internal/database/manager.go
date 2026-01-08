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
	ErrDatabaseNotFound        = errors.New("database not found")
	ErrDatabaseExists          = errors.New("database already exists")
	ErrUserExists              = errors.New("database user already exists")
	ErrMySQLNotInstalled       = errors.New("MySQL is not installed")
	ErrMySQLNotRunning         = errors.New("MySQL is not running")
	ErrPostgreSQLNotInstalled  = errors.New("PostgreSQL is not installed")
	ErrPostgreSQLNotRunning    = errors.New("PostgreSQL is not running")
	ErrUnsupportedDatabaseType = errors.New("unsupported database type")
)

// InstallStatus represents the MySQL installation status
type InstallStatus struct {
	InProgress bool      `json:"in_progress"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	Message    string    `json:"message,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
}

// Manager handles database operations
type Manager struct {
	databases     map[string]*models.Database
	mu            sync.RWMutex
	dataFile      string
	mysqlRootPwd  string
	pgSuperPwd    string
	installStatus map[string]*InstallStatus
	installMu     sync.RWMutex
}

// NewManager creates a new database manager
func NewManager() *Manager {
	cfg := config.Get()
	dataFile := filepath.Join(cfg.DataDir, "databases.json")

	m := &Manager{
		databases:     make(map[string]*models.Database),
		dataFile:      dataFile,
		installStatus: make(map[string]*InstallStatus),
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
	// Save root password if one was determined during installation
	if m.mysqlRootPwd != "" {
		_ = m.saveRootPassword(m.mysqlRootPwd)
	}

	return nil
}

// IsPostgreSQLInstalled checks if PostgreSQL is installed
func (m *Manager) IsPostgreSQLInstalled() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check for psql client
	if _, err := exec.LookPath("psql"); err != nil {
		return false
	}

	// Check if postgresql service exists
	cmd := exec.Command("systemctl", "list-unit-files", "postgresql.service")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "postgresql.service") {
		return true
	}

	// Check for postgresql binary
	if _, err := exec.LookPath("postgres"); err == nil {
		return true
	}

	// Check if postgresql package is installed
	cmd = exec.Command("dpkg-query", "-W", "-f=${Status}", "postgresql")
	if output, err := cmd.Output(); err == nil && strings.Contains(string(output), "install ok installed") {
		return true
	}

	return false
}

// IsPostgreSQLRunning checks if PostgreSQL is running
func (m *Manager) IsPostgreSQLRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "postgresql")
	return cmd.Run() == nil
}

// InstallPostgreSQL installs and configures PostgreSQL
func (m *Manager) InstallPostgreSQL() error {
	if runtime.GOOS != "linux" {
		return errors.New("PostgreSQL installation only supported on Linux")
	}

	log := func(msg string) {
		fmt.Printf("[PostgreSQL Install] %s\n", msg)
	}

	log("Starting PostgreSQL installation...")

	// Update package list
	log("Running apt-get update...")
	cmd := exec.Command("apt-get", "update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update packages: %w", err)
	}
	log("Package list updated")

	// Install PostgreSQL server
	log("Installing postgresql package (this may take a few minutes)...")
	cmd = exec.Command("apt-get", "install", "-y", "postgresql", "postgresql-contrib")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install PostgreSQL: %w", err)
	}
	log("PostgreSQL package installed")

	// Start PostgreSQL
	log("Starting PostgreSQL service...")
	cmd = exec.Command("systemctl", "start", "postgresql")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start PostgreSQL: %s - %s", err.Error(), string(output))
	}
	log("PostgreSQL service started")

	// Enable PostgreSQL to start on boot
	log("Enabling PostgreSQL on boot...")
	cmd = exec.Command("systemctl", "enable", "postgresql")
	_ = cmd.Run()

	log("PostgreSQL installation complete!")
	// Save superuser password if one was set during installation
	if m.pgSuperPwd != "" {
		_ = m.savePGPassword(m.pgSuperPwd)
	}
	return nil
}

// AdoptMySQL configures FastCP to work with an existing MySQL installation
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
			return fmt.Errorf("failed to start MySQL: %s", string(output))
		}
	}

	log("MySQL adoption complete!")
	return nil
}

// AdoptPostgreSQL configures FastCP to work with an existing PostgreSQL installation
func (m *Manager) AdoptPostgreSQL() error {
	if runtime.GOOS != "linux" {
		return errors.New("PostgreSQL adoption only supported on Linux")
	}

	log := func(msg string) {
		fmt.Printf("[PostgreSQL Adopt] %s\n", msg)
	}

	log("Adopting existing PostgreSQL installation...")

	// Make sure PostgreSQL is running
	if !m.IsPostgreSQLRunning() {
		log("Starting PostgreSQL service...")
		cmd := exec.Command("systemctl", "start", "postgresql")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start PostgreSQL: %s", string(output))
		}
	}

	// Test if we can connect as postgres user
	log("Testing PostgreSQL connection...")
	if err := m.execPG("", "SELECT 1;"); err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}

	log("PostgreSQL connection successful!")
	log("FastCP will use peer authentication for database operations.")

	return nil
}

// GetInstallStatus returns the current installation status for a database type
func (m *Manager) GetInstallStatus(dbType string) *InstallStatus {
	m.installMu.RLock()
	defer m.installMu.RUnlock()

	if status, ok := m.installStatus[dbType]; ok {
		return status
	}
	return &InstallStatus{InProgress: false}
}

// IsInstalling returns true if installation is in progress for the given database type
func (m *Manager) IsInstalling(dbType string) bool {
	m.installMu.RLock()
	defer m.installMu.RUnlock()
	if status, ok := m.installStatus[dbType]; ok {
		return status != nil && status.InProgress
	}
	return false
}

// InstallDatabaseAsync starts database installation in the background
func (m *Manager) InstallDatabaseAsync(dbType string) error {
	m.installMu.Lock()
	defer m.installMu.Unlock()

	// Check if already installing
	if status, ok := m.installStatus[dbType]; ok && status != nil && status.InProgress {
		return errors.New(dbType + " installation already in progress")
	}

	// Set initial status
	m.installStatus[dbType] = &InstallStatus{
		InProgress: true,
		Message:    "Starting " + dbType + " installation...",
		StartedAt:  time.Now(),
	}

	// Run installation in background
	go func() {
		var finalErr error

		// Check if already installed - if so, just adopt it
		var isInstalled bool
		switch dbType {
		case "mysql":
			isInstalled = m.IsMySQLInstalled()
			if isInstalled {
				m.updateInstallStatus("Adopting existing MySQL installation...", false, nil, dbType)
				finalErr = m.AdoptMySQL()
			} else {
				finalErr = m.InstallMySQL()
			}
		case "postgresql":
			isInstalled = m.IsPostgreSQLInstalled()
			if isInstalled {
				m.updateInstallStatus("Adopting existing PostgreSQL installation...", false, nil, dbType)
				finalErr = m.AdoptPostgreSQL()
			} else {
				finalErr = m.InstallPostgreSQL()
			}
		default:
			finalErr = ErrUnsupportedDatabaseType
		}

		// Update final status
		m.installMu.Lock()
		if finalErr != nil {
			m.installStatus[dbType] = &InstallStatus{
				InProgress: false,
				Success:    false,
				Error:      finalErr.Error(),
				Message:    dbType + " installation failed",
			}
		} else {
			m.installStatus[dbType] = &InstallStatus{
				InProgress: false,
				Success:    true,
				Message:    dbType + " installed successfully",
			}
		}
		m.installMu.Unlock()
	}()

	return nil
}

// updateInstallStatus updates the installation status message for a database type
func (m *Manager) updateInstallStatus(message string, inProgress bool, err error, dbType string) {
	m.installMu.Lock()
	defer m.installMu.Unlock()

	if m.installStatus[dbType] == nil {
		m.installStatus[dbType] = &InstallStatus{}
	}

	m.installStatus[dbType].Message = message
	m.installStatus[dbType].InProgress = inProgress
	if err != nil {
		m.installStatus[dbType].Error = err.Error()
		m.installStatus[dbType].Success = false
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

// savePGPassword saves the PostgreSQL superuser password securely
func (m *Manager) savePGPassword(password string) error {
	cfg := config.Get()
	pwdFile := filepath.Join(cfg.DataDir, ".pg_superuser_pwd")

	// Create with restricted permissions
	if err := os.WriteFile(pwdFile, []byte(password), 0600); err != nil {
		return err
	}

	return nil
}

// getPGPassword retrieves the PostgreSQL superuser password
func (m *Manager) getPGPassword() (string, error) {
	if m.pgSuperPwd != "" {
		return m.pgSuperPwd, nil
	}

	cfg := config.Get()
	pwdFile := filepath.Join(cfg.DataDir, ".pg_superuser_pwd")

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		return "", err
	}

	m.pgSuperPwd = strings.TrimSpace(string(data))
	return m.pgSuperPwd, nil
}

// EnsureDatabase ensures the specified database type is installed and running
func (m *Manager) EnsureDatabase(dbType string) error {
	switch dbType {
	case "mysql":
		return m.EnsureMySQL()
	case "postgresql":
		return m.EnsurePostgreSQL()
	default:
		return ErrUnsupportedDatabaseType
	}
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

// EnsurePostgreSQL ensures PostgreSQL is installed and running
func (m *Manager) EnsurePostgreSQL() error {
	if !m.IsPostgreSQLInstalled() {
		if err := m.InstallPostgreSQL(); err != nil {
			return err
		}
	}

	if !m.IsPostgreSQLRunning() {
		cmd := exec.Command("systemctl", "start", "postgresql")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start PostgreSQL: %s", string(output))
		}
	}

	return nil
}

// Create creates a new database and user
func (m *Manager) Create(db *models.Database) (*models.Database, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate database type
	if db.Type == "" {
		db.Type = "mysql" // default to MySQL for backward compatibility
	}
	if db.Type != "mysql" && db.Type != "postgresql" {
		return nil, ErrUnsupportedDatabaseType
	}

	// Ensure the database server is installed and running
	if err := m.EnsureDatabase(db.Type); err != nil {
		return nil, err
	}

	// Check if database already exists
	for _, existing := range m.databases {
		if existing.Name == db.Name && existing.Type == db.Type {
			return nil, ErrDatabaseExists
		}
	}

	// Generate ID and timestamps
	db.ID = generateID()
	db.CreatedAt = time.Now()
	db.UpdatedAt = time.Now()

	// Set default port if not specified
	if db.Port == 0 {
		switch db.Type {
		case "mysql":
			db.Port = 3306
		case "postgresql":
			db.Port = 5432
		}
	}

	// Generate password if not provided
	if db.Password == "" {
		db.Password = generatePassword(24)
	}

	// Create database based on type
	var err error
	switch db.Type {
	case "mysql":
		err = m.createMySQLDatabase(db)
	case "postgresql":
		err = m.createPostgreSQLDatabase(db)
	default:
		err = ErrUnsupportedDatabaseType
	}

	if err != nil {
		return nil, err
	}

	// Save to storage
	m.databases[db.ID] = db
	if err := m.save(); err != nil {
		return nil, err
	}

	return db, nil
}

// createMySQLDatabase creates a MySQL database and user
func (m *Manager) createMySQLDatabase(db *models.Database) error {
	// Get root password
	rootPwd, err := m.getRootPassword()
	if err != nil {
		// Try without password (socket auth)
		rootPwd = ""
	}

	// Create database
	if err := m.execMySQL(rootPwd, fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", db.Name)); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	// Create user for both localhost and 127.0.0.1 to handle both socket and TCP connections
	if err := m.execMySQL(rootPwd, fmt.Sprintf("CREATE USER '%s'@'127.0.0.1' IDENTIFIED BY '%s';", db.Username, db.Password)); err != nil {
		// Rollback: drop database
		m.execMySQL(rootPwd, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db.Name))
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Also create for localhost to handle socket connections
	_ = m.execMySQL(rootPwd, fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", db.Username, db.Password))

	// Grant privileges for both hosts
	if err := m.execMySQL(rootPwd, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'127.0.0.1';", db.Name, db.Username)); err != nil {
		// Rollback
		m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'127.0.0.1';", db.Username))
		m.execMySQL(rootPwd, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", db.Username))
		m.execMySQL(rootPwd, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db.Name))
		return fmt.Errorf("failed to grant privileges: %w", err)
	}

	// Grant for localhost too
	_ = m.execMySQL(rootPwd, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", db.Name, db.Username))

	// Flush privileges
	m.execMySQL(rootPwd, "FLUSH PRIVILEGES;")

	return nil
}

// createPostgreSQLDatabase creates a PostgreSQL database and user
func (m *Manager) createPostgreSQLDatabase(db *models.Database) error {
	// Get superuser password
	superPwd, err := m.getPGPassword()
	if err != nil {
		// Try without password (peer auth)
		superPwd = ""
	}

	// Create user
	if err := m.execPG(superPwd, fmt.Sprintf("CREATE USER \"%s\" WITH PASSWORD '%s';", db.Username, db.Password)); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Create database owned by the user
	if err := m.execPG(superPwd, fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\" ENCODING 'UTF8';", db.Name, db.Username)); err != nil {
		// Rollback: drop user
		m.execPG(superPwd, fmt.Sprintf("DROP USER IF EXISTS \"%s\";", db.Username))
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
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

	var err error
	switch db.Type {
	case "mysql":
		err = m.deleteMySQLDatabase(db)
	case "postgresql":
		err = m.deletePostgreSQLDatabase(db)
	default:
		return ErrUnsupportedDatabaseType
	}

	if err != nil {
		return err
	}

	// Remove from storage
	delete(m.databases, id)
	return m.save()
}

// deleteMySQLDatabase deletes a MySQL database and user
func (m *Manager) deleteMySQLDatabase(db *models.Database) error {
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

	return nil
}

// deletePostgreSQLDatabase deletes a PostgreSQL database and user
func (m *Manager) deletePostgreSQLDatabase(db *models.Database) error {
	superPwd, _ := m.getPGPassword()

	// Terminate active connections to the database
	m.execPG(superPwd, fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s';", db.Name))

	// Drop database
	if err := m.execPG(superPwd, fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", db.Name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Drop user
	m.execPG(superPwd, fmt.Sprintf("DROP USER IF EXISTS \"%s\";", db.Username))

	return nil
}

// UpdatePassword updates a database user's password
func (m *Manager) UpdatePassword(id, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	db, ok := m.databases[id]
	if !ok {
		return ErrDatabaseNotFound
	}

	var err error
	switch db.Type {
	case "mysql":
		err = m.updateMySQLPassword(db, newPassword)
	case "postgresql":
		err = m.updatePostgreSQLPassword(db, newPassword)
	default:
		return ErrUnsupportedDatabaseType
	}

	if err != nil {
		return err
	}

	db.Password = newPassword
	db.UpdatedAt = time.Now()

	return m.save()
}

// updateMySQLPassword updates a MySQL user's password
func (m *Manager) updateMySQLPassword(db *models.Database, newPassword string) error {
	rootPwd, _ := m.getRootPassword()

	// Update password for both hosts
	if err := m.execMySQL(rootPwd, fmt.Sprintf("ALTER USER '%s'@'127.0.0.1' IDENTIFIED BY '%s';", db.Username, newPassword)); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	// Also update localhost user if it exists
	_ = m.execMySQL(rootPwd, fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", db.Username, newPassword))

	m.execMySQL(rootPwd, "FLUSH PRIVILEGES;")

	return nil
}

// updatePostgreSQLPassword updates a PostgreSQL user's password
func (m *Manager) updatePostgreSQLPassword(db *models.Database, newPassword string) error {
	superPwd, _ := m.getPGPassword()

	// Update password
	if err := m.execPG(superPwd, fmt.Sprintf("ALTER USER \"%s\" PASSWORD '%s';", db.Username, newPassword)); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	return nil
}

// GetStatus returns database server status for all supported types
func (m *Manager) GetStatus() *models.DatabaseServerStatus {
	status := &models.DatabaseServerStatus{}

	// MySQL status
	status.MySQL.Installed = m.IsMySQLInstalled()
	status.MySQL.Running = m.IsMySQLRunning()
	if status.MySQL.Running {
		// Get version
		cmd := exec.Command("mysql", "-V")
		if output, err := cmd.Output(); err == nil {
			status.MySQL.Version = strings.TrimSpace(string(output))
		}
		// Count MySQL databases
		mysqlCount := 0
		for _, db := range m.databases {
			if db.Type == "mysql" {
				mysqlCount++
			}
		}
		status.MySQL.DatabaseCount = mysqlCount
	}

	// PostgreSQL status
	status.PostgreSQL.Installed = m.IsPostgreSQLInstalled()
	status.PostgreSQL.Running = m.IsPostgreSQLRunning()
	if status.PostgreSQL.Running {
		// Get version
		cmd := exec.Command("psql", "--version")
		if output, err := cmd.Output(); err == nil {
			status.PostgreSQL.Version = strings.TrimSpace(string(output))
		}
		// Count PostgreSQL databases
		pgCount := 0
		for _, db := range m.databases {
			if db.Type == "postgresql" {
				pgCount++
			}
		}
		status.PostgreSQL.DatabaseCount = pgCount
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

// execPG executes a PostgreSQL query as postgres superuser
// It tries password auth first (if password provided), then falls back to peer auth
func (m *Manager) execPG(superPwd, query string) error {
	var cmd *exec.Cmd
	var output []byte
	var err error

	// Try with password first if available
	if superPwd != "" {
		cmd = exec.Command("psql", "-U", "postgres", fmt.Sprintf("-c %s", query))
		cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", superPwd))
		output, err = cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		// Password auth failed, fall through to peer auth
	}

	// Try peer auth (works when running as root or postgres user)
	cmd = exec.Command("sudo", "-u", "postgres", "psql", "-c", query)
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
