//go:build linux

package agent

import (
	"bufio"
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

const (
	homeBase      = "/home"
	appsDir       = "apps"
	fastcpDir     = ".fastcp"
	wpDownloadURL = "https://wordpress.org/latest.tar.gz"
	caddyConfig   = "/opt/fastcp/config/Caddyfile"
	mysqlSocket   = "/var/run/mysqld/mysqld.sock"
	fastcpRunDir  = "/opt/fastcp/run"
)

// runStartupMigrations fixes configuration drift on agent startup (e.g. after updates).
func (s *Server) runStartupMigrations() {
	s.ensureRunDir()
	s.ensurePHPIniConfig()
	s.ensureServiceFiles()
	s.ensurePMAConfig()
	s.ensureUserSocketDirs()
	s.cleanStaleSocketsAndReload()
}

func (s *Server) ensureRunDir() {
	os.MkdirAll(fastcpRunDir, 0755)

	// Clean up old tmpfs-based runtime dir
	os.Remove("/etc/tmpfiles.d/fastcp.conf")
}

func (s *Server) ensurePHPIniConfig() {
	iniDir := "/opt/fastcp/config/php"
	iniFile := filepath.Join(iniDir, "99-fastcp.ini")
	if _, err := os.Stat(iniFile); err == nil {
		return
	}
	os.MkdirAll(iniDir, 0755)
	os.WriteFile(iniFile, []byte("display_errors = Off\nerror_reporting = 22527\n"), 0644)
	os.Remove("/opt/fastcp/phpmyadmin/.user.ini")
	slog.Info("created PHP ini config", "path", iniFile)
}

func (s *Server) ensureServiceFiles() {
	needsReload := false

	// Migrate fastcp-agent.service: remove RuntimeDirectory (no longer needed)
	agentUnit := "/etc/systemd/system/fastcp-agent.service"
	if data, err := os.ReadFile(agentUnit); err == nil {
		content := string(data)
		if strings.Contains(content, "RuntimeDirectory=") {
			content = strings.Replace(content, "RuntimeDirectory=fastcp\n", "", 1)
			content = strings.Replace(content, "RuntimeDirectoryMode=1777\n", "", 1)
			content = strings.Replace(content, "RuntimeDirectoryPreserve=yes\n", "", 1)
			os.WriteFile(agentUnit, []byte(content), 0644)
			needsReload = true
			slog.Info("removed RuntimeDirectory from fastcp-agent.service")
		}
	}

	// Migrate fastcp.service: update socket path
	mainUnit := "/etc/systemd/system/fastcp.service"
	if data, err := os.ReadFile(mainUnit); err == nil {
		content := string(data)
		if strings.Contains(content, "/var/run/fastcp/") {
			content = strings.ReplaceAll(content, "/var/run/fastcp/", "/opt/fastcp/run/")
			os.WriteFile(mainUnit, []byte(content), 0644)
			needsReload = true
			slog.Info("migrated fastcp.service socket path to /opt/fastcp/run/")
		}
	}

	// Ensure fastcp-caddy.service has PHP_INI_SCAN_DIR
	caddyUnit := "/etc/systemd/system/fastcp-caddy.service"
	if data, err := os.ReadFile(caddyUnit); err == nil {
		content := string(data)
		if !strings.Contains(content, "PHP_INI_SCAN_DIR") {
			content = strings.Replace(content, "RestartSec=5\n", "RestartSec=5\nEnvironment=PHP_INI_SCAN_DIR=:/opt/fastcp/config/php\n", 1)
			os.WriteFile(caddyUnit, []byte(content), 0644)
			needsReload = true
		}
	}

	// Migrate all fastcp-php service files: remove shared run dir from ReadWritePaths
	phpUnits, _ := filepath.Glob("/etc/systemd/system/fastcp-php@*.service")
	for _, phpUnit := range phpUnits {
		data, err := os.ReadFile(phpUnit)
		if err != nil {
			continue
		}
		content := string(data)
		changed := false
		if strings.Contains(content, "/var/run/fastcp") {
			content = strings.ReplaceAll(content, "/var/run/fastcp", "/opt/fastcp/run")
			changed = true
		}
		if strings.Contains(content, " /opt/fastcp/run") {
			content = strings.ReplaceAll(content, " /opt/fastcp/run", "")
			changed = true
		}
		if changed {
			os.WriteFile(phpUnit, []byte(content), 0644)
			needsReload = true
			slog.Info("migrated php service", "unit", filepath.Base(phpUnit))
		}
	}

	if needsReload {
		exec.Command("systemctl", "daemon-reload").Run()
	}
}

func (s *Server) ensureUserSocketDirs() {
	// Check if any old-style sockets exist in the shared dir -- if so, we need to migrate
	oldSockets, _ := filepath.Glob(filepath.Join(fastcpRunDir, "php-*.sock"))
	if len(oldSockets) == 0 {
		// Also check if any user config dirs exist without the new socket dir
		userDirs, _ := filepath.Glob("/opt/fastcp/config/users/*")
		needsMigration := false
		for _, dir := range userDirs {
			username := filepath.Base(dir)
			sockDir := userSocketDir(username)
			if _, err := os.Stat(sockDir); err != nil {
				needsMigration = true
				break
			}
		}
		if !needsMigration {
			return
		}
	}

	slog.Info("migrating user socket directories to home directories")

	// Create ~/.fastcp/run/ for all existing users with config dirs
	userDirs, _ := filepath.Glob("/opt/fastcp/config/users/*")
	for _, dir := range userDirs {
		username := filepath.Base(dir)
		u, err := user.Lookup(username)
		if err != nil {
			continue
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		sockDir := userSocketDir(username)
		os.MkdirAll(sockDir, 0755)
		os.Chown(sockDir, uid, gid)
		// Also ensure parent .fastcp dir has correct ownership
		os.Chown(filepath.Dir(sockDir), uid, gid)
	}

	// Remove old sockets so user PHP processes restart with new paths
	for _, sock := range oldSockets {
		os.Remove(sock)
	}
	oldPids, _ := filepath.Glob(filepath.Join(fastcpRunDir, "php-*.pid"))
	for _, pid := range oldPids {
		os.Remove(pid)
	}

	// Kill old user FrankenPHP processes so they restart with new socket paths
	exec.Command("pkill", "-f", "frankenphp run --config /opt/fastcp/config/users").Run()
	time.Sleep(1 * time.Second)

	// Regenerate Caddyfiles with new socket paths and reload
	if err := s.generateCaddyfile(); err != nil {
		slog.Error("failed to regenerate Caddyfile during migration", "error", err)
	} else {
		exec.Command("pkill", "-USR1", "frankenphp").Run()
		slog.Info("regenerated Caddyfile with new user socket paths")
	}
}

func (s *Server) cleanStaleSocketsAndReload() {
	// After a reboot, socket files persist on disk but the processes are dead.
	// Remove stale sockets so FrankenPHP can bind fresh, then regenerate and reload.
	userDirs, _ := filepath.Glob("/opt/fastcp/config/users/*")
	for _, dir := range userDirs {
		username := filepath.Base(dir)
		sockFile := userSocketPath(username)
		if _, err := os.Stat(sockFile); err != nil {
			continue
		}
		// Socket file exists -- check if a process is actually listening
		conn, err := net.Dial("unix", sockFile)
		if err != nil {
			// Can't connect: stale socket from a previous boot
			os.Remove(sockFile)
			pidFile := filepath.Join(userSocketDir(username), "php.pid")
			os.Remove(pidFile)
			slog.Info("removed stale socket", "username", username)
		} else {
			conn.Close()
		}
	}

	// Regenerate Caddyfile and start any stopped user PHP processes
	if err := s.generateCaddyfile(); err != nil {
		slog.Error("failed to regenerate Caddyfile on startup", "error", err)
	} else {
		exec.Command("pkill", "-USR1", "frankenphp").Run()
	}
}

func (s *Server) ensurePMAConfig() {
	configFile := "/opt/fastcp/phpmyadmin/config.inc.php"
	data, err := os.ReadFile(configFile)
	if err != nil {
		return
	}
	content := string(data)
	changed := false
	if strings.Contains(content, "'host'] = 'localhost'") {
		content = strings.Replace(content, "'host'] = 'localhost'", "'host'] = '127.0.0.1'", 1)
		changed = true
	}
	if !strings.Contains(content, "ShowCreateDb") {
		content = strings.Replace(content, "$cfg['LoginCookieValidity']", "$cfg['ShowCreateDb'] = false;\n$cfg['LoginCookieValidity']", 1)
		changed = true
	}
	if changed {
		os.WriteFile(configFile, []byte(content), 0644)
		slog.Info("patched phpMyAdmin config")
	}
}

// Site handlers

func (s *Server) handleCreateSiteDirectory(ctx context.Context, params json.RawMessage) (any, error) {
	var req CreateSiteDirectoryRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("creating site directory", "username", req.Username, "domain", req.Domain)

	// Get user info
	u, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Create directory structure using the slug
	siteDir := filepath.Join(homeBase, req.Username, appsDir, req.Slug)
	dirs := []string{
		siteDir,
		filepath.Join(siteDir, "public"),
		filepath.Join(siteDir, "logs"),
		filepath.Join(siteDir, "tmp"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create default index.php with beautiful welcome page
	indexPath := filepath.Join(siteDir, "public", "index.php")
	indexContent := fmt.Sprintf(`<?php
$domain = '%s';
$docRoot = '%s';
?>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Welcome - <?php echo $domain; ?></title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 24px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
            padding: 48px;
            max-width: 600px;
            width: 100%%;
            text-align: center;
        }
        .logo {
            width: 80px;
            height: 80px;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            border-radius: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 24px;
        }
        .logo svg { width: 40px; height: 40px; }
        h1 {
            font-size: 28px;
            font-weight: 700;
            color: #1a202c;
            margin-bottom: 8px;
        }
        .domain {
            font-size: 16px;
            color: #667eea;
            font-weight: 500;
            margin-bottom: 32px;
        }
        .status {
            display: inline-flex;
            align-items: center;
            padding: 8px 16px;
            background: #f0fdf4;
            border: 1px solid #bbf7d0;
            border-radius: 100px;
            color: #166534;
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 32px;
        }
        .status::before {
            content: '';
            width: 8px;
            height: 8px;
            background: #22c55e;
            border-radius: 50%%;
            margin-right: 8px;
        }
        .instructions {
            text-align: left;
            background: #f8fafc;
            border-radius: 16px;
            padding: 24px;
            margin-bottom: 32px;
        }
        .instructions h2 {
            font-size: 16px;
            font-weight: 600;
            color: #334155;
            margin-bottom: 16px;
        }
        .instructions ul {
            list-style: none;
        }
        .instructions li {
            display: flex;
            align-items: flex-start;
            padding: 12px 0;
            border-bottom: 1px solid #e2e8f0;
            font-size: 14px;
            color: #475569;
        }
        .instructions li:last-child { border-bottom: none; }
        .instructions .num {
            width: 24px;
            height: 24px;
            background: #667eea;
            color: white;
            border-radius: 50%%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 12px;
            font-weight: 600;
            margin-right: 12px;
            flex-shrink: 0;
        }
        .path {
            background: #1e293b;
            color: #e2e8f0;
            padding: 12px 16px;
            border-radius: 8px;
            font-family: 'SF Mono', Monaco, monospace;
            font-size: 13px;
            margin-top: 8px;
            word-break: break-all;
        }
        .footer {
            font-size: 13px;
            color: #94a3b8;
        }
        .footer a {
            color: #667eea;
            text-decoration: none;
            font-weight: 500;
        }
        .footer a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo">
            <svg fill="none" stroke="white" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2"></path>
            </svg>
        </div>
        
        <h1>Your Site is Ready!</h1>
        <p class="domain"><?php echo $domain; ?></p>
        
        <div class="status">Site is live and running</div>
        
        <div class="instructions">
            <h2>Getting Started</h2>
            <ul>
                <li>
                    <span class="num">1</span>
                    <div>
                        <strong>Upload your files</strong><br>
                        Use SFTP or SSH to upload your website files
                        <div class="path"><?php echo $docRoot; ?></div>
                    </div>
                </li>
                <li>
                    <span class="num">2</span>
                    <div>
                        <strong>Replace this page</strong><br>
                        Upload your own index.php or index.html to replace this welcome page
                    </div>
                </li>
                <li>
                    <span class="num">3</span>
                    <div>
                        <strong>Configure your application</strong><br>
                        Set up your database connection and environment variables as needed
                    </div>
                </li>
            </ul>
        </div>
        
        <p class="footer">
            Powered by <a href="https://github.com/rehmatworks/fastcp" target="_blank">FastCP</a>
        </p>
    </div>
</body>
</html>
`, req.Domain, filepath.Join(siteDir, "public"))

	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to create index.php: %w", err)
	}

	// Set ownership recursively
	if err := chownRecursive(siteDir, uid, gid); err != nil {
		return nil, fmt.Errorf("failed to set ownership: %w", err)
	}

	// Set ACLs
	if err := setACLs(siteDir, req.Username); err != nil {
		slog.Warn("failed to set ACLs", "error", err)
	}

	return map[string]string{"status": "ok", "path": siteDir}, nil
}

func (s *Server) handleDeleteSiteDirectory(ctx context.Context, params json.RawMessage) (any, error) {
	var req DeleteSiteDirectoryRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("deleting site directory", "username", req.Username, "slug", req.Slug)

	siteDir := filepath.Join(homeBase, req.Username, appsDir, req.Slug)

	// Verify path is within user's home
	if !strings.HasPrefix(siteDir, filepath.Join(homeBase, req.Username)) {
		return nil, fmt.Errorf("invalid path")
	}

	if err := os.RemoveAll(siteDir); err != nil {
		return nil, fmt.Errorf("failed to delete directory: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleInstallWordPress(ctx context.Context, params json.RawMessage) (any, error) {
	var req InstallWordPressRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("installing WordPress", "username", req.Username, "domain", req.Domain)

	// Get user info
	u, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Download WordPress
	tmpFile := filepath.Join("/tmp", fmt.Sprintf("wp-%s.tar.gz", req.Username))
	if err := exec.Command("curl", "-sL", "-o", tmpFile, wpDownloadURL).Run(); err != nil {
		return nil, fmt.Errorf("failed to download WordPress: %w", err)
	}
	defer os.Remove(tmpFile)

	// Extract WordPress to public directory
	publicDir := req.Path
	if err := exec.Command("tar", "-xzf", tmpFile, "-C", publicDir, "--strip-components=1").Run(); err != nil {
		return nil, fmt.Errorf("failed to extract WordPress: %w", err)
	}

	// Create database for WordPress
	slog.Info("creating WordPress database", "db_name", req.DBName, "db_user", req.DBUser)
	db, err := sql.Open("mysql", fmt.Sprintf("root@unix(%s)/", mysqlSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Create database
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", req.DBName))
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Create user for both localhost and 127.0.0.1 (MySQL treats them differently)
	for _, host := range []string{"localhost", "127.0.0.1"} {
		_, err = db.Exec(fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", req.DBUser, host, req.DBPass))
		if err != nil {
			return nil, fmt.Errorf("failed to create database user: %w", err)
		}
		_, err = db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", req.DBName, req.DBUser, host))
		if err != nil {
			return nil, fmt.Errorf("failed to grant privileges: %w", err)
		}
	}
	db.Exec("FLUSH PRIVILEGES")

	// Generate wp-config.php
	wpConfig := generateWPConfig(req.DBName, req.DBUser, req.DBPass)
	wpConfigPath := filepath.Join(publicDir, "wp-config.php")
	if err := os.WriteFile(wpConfigPath, []byte(wpConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write wp-config.php: %w", err)
	}

	// Set ownership for the entire site directory
	siteDir := filepath.Dir(publicDir)
	if err := chownRecursive(siteDir, uid, gid); err != nil {
		return nil, fmt.Errorf("failed to set ownership: %w", err)
	}

	return &InstallWordPressResponse{
		Status: "ok",
		DBName: req.DBName,
		DBUser: req.DBUser,
		DBPass: req.DBPass,
	}, nil
}

func generateWPConfig(dbName, dbUser, dbPass string) string {
	// Generate random salts
	salts := make([]string, 8)
	for i := range salts {
		salts[i] = generateRandomString(64)
	}

	return fmt.Sprintf(`<?php
/**
 * WordPress Configuration - Generated by FastCP
 */

// Database settings
define( 'DB_NAME', '%s' );
define( 'DB_USER', '%s' );
define( 'DB_PASSWORD', '%s' );
define( 'DB_HOST', '127.0.0.1' );
define( 'DB_CHARSET', 'utf8mb4' );
define( 'DB_COLLATE', '' );

// Authentication keys and salts
define( 'AUTH_KEY',         '%s' );
define( 'SECURE_AUTH_KEY',  '%s' );
define( 'LOGGED_IN_KEY',    '%s' );
define( 'NONCE_KEY',        '%s' );
define( 'AUTH_SALT',        '%s' );
define( 'SECURE_AUTH_SALT', '%s' );
define( 'LOGGED_IN_SALT',   '%s' );
define( 'NONCE_SALT',       '%s' );

// Database table prefix
$table_prefix = 'wp_';

// HTTPS and SSL handling (auto-detect from reverse proxy)
if ( isset( $_SERVER['HTTP_X_FORWARDED_PROTO'] ) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https' ) {
	$_SERVER['HTTPS'] = 'on';
}
if ( isset( $_SERVER['HTTP_X_FORWARDED_SSL'] ) && $_SERVER['HTTP_X_FORWARDED_SSL'] === 'on' ) {
	$_SERVER['HTTPS'] = 'on';
}

// Force SSL for admin
define( 'FORCE_SSL_ADMIN', true );

// Allow WordPress to detect the correct URL scheme
if ( isset( $_SERVER['HTTPS'] ) && $_SERVER['HTTPS'] === 'on' ) {
	define( 'WP_HOME', 'https://' . $_SERVER['HTTP_HOST'] );
	define( 'WP_SITEURL', 'https://' . $_SERVER['HTTP_HOST'] );
}

// Debugging (set to true to enable)
define( 'WP_DEBUG', false );

// Absolute path to WordPress directory
if ( ! defined( 'ABSPATH' ) ) {
	define( 'ABSPATH', __DIR__ . '/' );
}

// Load WordPress
require_once ABSPATH . 'wp-settings.php';
`, dbName, dbUser, dbPass,
		salts[0], salts[1], salts[2], salts[3],
		salts[4], salts[5], salts[6], salts[7])
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
	b := make([]byte, length)
	randomBytes := make([]byte, length)
	// Use crypto/rand for secure random
	if _, err := cryptoRand.Read(randomBytes); err != nil {
		// Fallback to less secure but still random
		for i := range b {
			b[i] = charset[i%len(charset)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = charset[int(randomBytes[i])%len(charset)]
	}
	return string(b)
}

// Caddy handlers

func (s *Server) handleReloadCaddy(ctx context.Context, params json.RawMessage) (any, error) {
	slog.Info("regenerating and reloading Caddy configuration")

	// Generate Caddyfile from database
	if err := s.generateCaddyfile(); err != nil {
		return nil, fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	// Check if FrankenPHP is running
	if !s.isFrankenPHPRunning() {
		slog.Info("FrankenPHP not running, starting it")
		if err := s.startFrankenPHP(); err != nil {
			return nil, fmt.Errorf("failed to start FrankenPHP: %w", err)
		}
		return map[string]string{"status": "ok", "action": "started"}, nil
	}

	// Use Caddy's admin API to reload
	cmd := exec.Command("curl", "-s", "-X", "POST", "http://localhost:2019/load",
		"-H", "Content-Type: text/caddyfile",
		"--data-binary", fmt.Sprintf("@%s", caddyConfig))

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Admin API failed, FrankenPHP might have crashed - restart it
		slog.Warn("admin API reload failed, restarting FrankenPHP", "error", err, "output", string(output))
		if err := s.startFrankenPHP(); err != nil {
			return nil, fmt.Errorf("failed to restart FrankenPHP: %w", err)
		}
		return map[string]string{"status": "ok", "action": "restarted"}, nil
	}

	return map[string]string{"status": "ok", "action": "reloaded"}, nil
}

func (s *Server) isFrankenPHPRunning() bool {
	cmd := exec.Command("pgrep", "-x", "frankenphp")
	err := cmd.Run()
	return err == nil
}

func (s *Server) startFrankenPHP() error {
	// Kill any existing (possibly zombie) processes
	exec.Command("pkill", "-9", "frankenphp").Run()
	
	// Give it a moment
	exec.Command("sleep", "1").Run()

	// Start FrankenPHP in background
	cmd := exec.Command("/usr/local/bin/frankenphp", "run", "--config", caddyConfig)
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FrankenPHP: %w", err)
	}

	// Don't wait for it - let it run in background
	go cmd.Wait()

	// Give it time to start
	exec.Command("sleep", "2").Run()

	slog.Info("FrankenPHP started", "pid", cmd.Process.Pid)
	return nil
}

func (s *Server) hasSystemd() bool {
	// Check if systemd is available and running
	cmd := exec.Command("systemctl", "is-system-running")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	state := strings.TrimSpace(string(output))
	return state == "running" || state == "degraded"
}

func userSocketDir(username string) string {
	return filepath.Join(homeBase, username, fastcpDir, "run")
}

func userSocketPath(username string) string {
	return filepath.Join(userSocketDir(username), "php.sock")
}

func (s *Server) startUserPHP(username string) error {
	sockDir := userSocketDir(username)
	socketPath := userSocketPath(username)
	pidFile := filepath.Join(sockDir, "php.pid")
	configPath := fmt.Sprintf("/opt/fastcp/config/users/%s/Caddyfile", username)
	logPath := fmt.Sprintf("/var/log/fastcp/frankenphp-%s.log", username)
	phpIniDir := filepath.Dir(configPath)

	// Check if already running
	if _, err := os.Stat(socketPath); err == nil {
		slog.Info("user PHP already has socket", "username", username)
		return nil
	}

	// Ensure user socket directory exists with correct ownership
	os.MkdirAll(sockDir, 0755)
	if u, err := user.Lookup(username); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		os.Chown(sockDir, uid, gid)
	}

	// Ensure log file exists and is writable
	os.MkdirAll("/var/log/fastcp", 0777)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		slog.Warn("failed to create log file", "error", err)
	} else {
		logFile.Close()
	}

	cmd := exec.Command("start-stop-daemon",
		"--start",
		"--background",
		"--make-pidfile",
		"--pidfile", pidFile,
		"--chuid", username,
		"--exec", "/usr/local/bin/frankenphp",
		"--",
		"run",
		"--config", configPath,
	)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PHP_INI_SCAN_DIR=%s", phpIniDir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("start-stop-daemon failed, trying fallback", "error", err, "output", string(output))

		// Fallback: use shell backgrounding with disown
		fallbackCmd := fmt.Sprintf(
			"su -s /bin/bash %s -c 'export PHP_INI_SCAN_DIR=%s; nohup /usr/local/bin/frankenphp run --config %s >> %s 2>&1 & disown'",
			username, phpIniDir, configPath, logPath,
		)
		cmd2 := exec.Command("bash", "-c", fallbackCmd)
		if err2 := cmd2.Run(); err2 != nil {
			return fmt.Errorf("failed to start FrankenPHP for user %s: %w (fallback also failed: %v)", username, err, err2)
		}
	}

	// Wait a bit for socket to appear
	time.Sleep(2 * time.Second)

	slog.Info("started user PHP process", "username", username)
	return nil
}

func (s *Server) generateCaddyfile() error {
	// Ensure suspended page exists
	s.ensureSuspendedPage()

	// Open FastCP database to get sites
	db, err := sql.Open("sqlite3", "/opt/fastcp/data/fastcp.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Fetch suspended users
	suspendedUsers := make(map[string]bool)
	suspendedRows, err := db.Query("SELECT username FROM users WHERE is_suspended = 1")
	if err == nil {
		defer suspendedRows.Close()
		for suspendedRows.Next() {
			var username string
			if err := suspendedRows.Scan(&username); err == nil {
				suspendedUsers[username] = true
			}
		}
	}

	// Fetch all sites
	rows, err := db.Query("SELECT id, domain, username, document_root FROM sites")
	if err != nil {
		// If no sites table yet, use default config
		slog.Warn("no sites table found, using default config", "error", err)
		return nil
	}
	defer rows.Close()

	// Build sites map
	sitesMap := make(map[string]*siteInfo)
	for rows.Next() {
		var site siteInfo
		if err := rows.Scan(&site.ID, &site.Domain, &site.Username, &site.DocumentRoot); err != nil {
			continue
		}
		site.SafeDomain = strings.ReplaceAll(site.Domain, ".", "_")
		site.PrimaryDomain = site.Domain // Default to main domain
		site.Domains = []siteDomainInfo{{Domain: site.Domain, IsPrimary: true, RedirectToPrimary: false}}
		site.IsSuspended = suspendedUsers[site.Username]
		sitesMap[site.ID] = &site
	}
	rows.Close()

	// Fetch all site domains
	sitesWithDomains := make(map[string]bool) // Track which sites have entries in site_domains
	domainRows, err := db.Query("SELECT site_id, domain, is_primary, COALESCE(redirect_to_primary, 0) FROM site_domains ORDER BY is_primary DESC")
	if err == nil {
		defer domainRows.Close()
		for domainRows.Next() {
			var siteID, domain string
			var isPrimary, redirectToPrimary bool
			if err := domainRows.Scan(&siteID, &domain, &isPrimary, &redirectToPrimary); err != nil {
				continue
			}
			if site, ok := sitesMap[siteID]; ok {
				// Clear default domains only once when we first see this site in site_domains
				if !sitesWithDomains[siteID] {
					site.Domains = nil
					sitesWithDomains[siteID] = true
				}
				site.Domains = append(site.Domains, siteDomainInfo{
					Domain:            domain,
					IsPrimary:         isPrimary,
					RedirectToPrimary: redirectToPrimary,
				})
				if isPrimary {
					site.PrimaryDomain = domain
				}
			}
		}
	}

	// Group sites by username
	sitesByUser := make(map[string][]siteInfo)
	for _, site := range sitesMap {
		sitesByUser[site.Username] = append(sitesByUser[site.Username], *site)
	}

	useSystemd := s.hasSystemd()

	// Generate main Caddyfile (reverse proxy to user sockets)
	var mainBuf strings.Builder
	mainBuf.WriteString(`# FastCP Main Caddyfile - Reverse Proxy
# DO NOT EDIT - This file is auto-generated by FastCP

{
    admin localhost:2019
}

`)

	for username, sites := range sitesByUser {
		socketPath := userSocketPath(username)
		isSuspended := len(sites) > 0 && sites[0].IsSuspended

		for _, site := range sites {
			logsDir := filepath.Join(homeBase, username, appsDir, site.SafeDomain, "logs")
			os.MkdirAll(logsDir, 0755)

			// Generate blocks for each domain
			for _, domain := range site.Domains {
				if site.IsSuspended {
					// Serve suspended page for suspended users
					mainBuf.WriteString(fmt.Sprintf(`# Site: %s (User: %s) [SUSPENDED]
%s {
    root * /var/www/suspended
    file_server
    try_files {path} /index.html
    
    log {
        output file %s/access.log
    }
}

`, domain.Domain, username, domain.Domain, logsDir))
				} else if domain.RedirectToPrimary && site.PrimaryDomain != domain.Domain {
					// Generate redirect block for non-primary domains that should redirect
					mainBuf.WriteString(fmt.Sprintf(`# Redirect: %s -> %s (User: %s)
%s {
    redir https://%s{uri} permanent
}

`, domain.Domain, site.PrimaryDomain, username, domain.Domain, site.PrimaryDomain))
				} else {
					// Generate reverse proxy block for domains that serve content
					mainBuf.WriteString(fmt.Sprintf(`# Site: %s (User: %s)%s
%s {
    reverse_proxy unix/%s {
        header_up X-Forwarded-Proto {scheme}
    }
    
    log {
        output file %s/access.log
    }
}

`, domain.Domain, username, func() string {
						if domain.IsPrimary {
							return " [PRIMARY]"
						}
						return ""
					}(), domain.Domain, socketPath, logsDir))
				}
			}
		}

		// Generate per-user Caddyfile
		if err := s.generateUserCaddyfile(username, sites); err != nil {
			slog.Warn("failed to generate user Caddyfile", "username", username, "error", err)
		}

		// Start user's PHP service (skip if suspended)
		if len(sites) > 0 && !isSuspended {
			if useSystemd {
				serviceName := fmt.Sprintf("fastcp-php@%s.service", username)
				exec.Command("systemctl", "start", serviceName).Run()
			} else {
				// Without systemd, start process directly
				if err := s.startUserPHP(username); err != nil {
					slog.Warn("failed to start user PHP", "username", username, "error", err)
				}
			}
		}
	}

	// Add phpMyAdmin (internal only - accessed via FastCP reverse proxy)
	mainBuf.WriteString(`# phpMyAdmin (internal only - accessed via FastCP reverse proxy)
http://localhost:8088 {
    root * /opt/fastcp/phpmyadmin
    php_server
}

# Default fallback for unconfigured domains
:80, :443 {
    respond "FastCP - No site configured for this domain" 404
}
`)

	// Write main Caddyfile
	if err := os.WriteFile(caddyConfig, []byte(mainBuf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write main Caddyfile: %w", err)
	}

	slog.Info("generated Caddyfiles", "users", len(sitesByUser), "systemd", useSystemd)
	return nil
}

func (s *Server) ensureSuspendedPage() {
	suspendedDir := "/var/www/suspended"
	suspendedHTML := filepath.Join(suspendedDir, "index.html")

	// Create directory if it doesn't exist
	os.MkdirAll(suspendedDir, 0755)

	// Check if file already exists
	if _, err := os.Stat(suspendedHTML); err == nil {
		return
	}

	// Create the suspended page
	content := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Account Suspended</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 16px;
            padding: 48px;
            text-align: center;
            max-width: 500px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
        }
        .icon {
            width: 80px;
            height: 80px;
            background: #fee2e2;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 24px;
        }
        .icon svg {
            width: 40px;
            height: 40px;
            color: #dc2626;
        }
        h1 {
            color: #1f2937;
            font-size: 28px;
            margin-bottom: 12px;
        }
        p {
            color: #6b7280;
            font-size: 16px;
            line-height: 1.6;
            margin-bottom: 24px;
        }
        .contact {
            background: #f3f4f6;
            border-radius: 8px;
            padding: 16px;
            color: #374151;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">
            <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
            </svg>
        </div>
        <h1>Account Suspended</h1>
        <p>This website has been temporarily suspended. If you believe this is an error, please contact the server administrator.</p>
        <div class="contact">
            For assistance, please contact your hosting provider.
        </div>
    </div>
</body>
</html>`

	os.WriteFile(suspendedHTML, []byte(content), 0644)
}

func (s *Server) generateUserCaddyfile(username string, sites []siteInfo) error {
	userConfigDir := filepath.Join("/opt/fastcp/config/users", username)
	os.MkdirAll(userConfigDir, 0755)

	socketPath := userSocketPath(username)

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf(`# FrankenPHP config for user: %s
# DO NOT EDIT - This file is auto-generated by FastCP

{
    admin off
    
    frankenphp {
        num_threads 4
    }
}

:80 {
    bind unix/%s

`, username, socketPath))

	for _, site := range sites {
		// Get all domains that should serve content (not redirecting)
		var servingDomains []string
		for _, domain := range site.Domains {
			if !domain.RedirectToPrimary {
				servingDomains = append(servingDomains, domain.Domain)
			}
		}
		// Fallback to main domain if no domains configured
		if len(servingDomains) == 0 {
			servingDomains = []string{site.Domain}
		}

		matcherName := strings.ReplaceAll(site.SafeDomain, "-", "_")
		hostList := strings.Join(servingDomains, " ")
		buf.WriteString(fmt.Sprintf(`    @%s host %s
    handle @%s {
        root * %s
        php_server
    }

`, matcherName, hostList, matcherName, site.DocumentRoot))
	}

	// Create per-user temp directory
	userTmpDir := filepath.Join(homeBase, username, ".tmp")
	userSessionDir := filepath.Join(userTmpDir, "sessions")
	userUploadDir := filepath.Join(userTmpDir, "uploads")

	// Get user info for ownership
	u, err := user.Lookup(username)
	if err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		for _, dir := range []string{userTmpDir, userSessionDir, userUploadDir} {
			os.MkdirAll(dir, 0700)
			os.Chown(dir, uid, gid)
		}
	}

	// Create per-user php.ini with security settings
	var docRoots []string
	for _, site := range sites {
		docRoots = append(docRoots, filepath.Dir(site.DocumentRoot))
		docRoots = append(docRoots, site.DocumentRoot)
	}
	// Include user's own temp directory, NOT shared /tmp
	openBasedir := strings.Join(docRoots, ":") + ":" + userTmpDir

	// Additional cache directories
	userCacheDir := filepath.Join(userTmpDir, "cache")
	userWsdlDir := filepath.Join(userTmpDir, "wsdl")
	os.MkdirAll(userCacheDir, 0700)
	os.MkdirAll(userWsdlDir, 0700)
	if err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		os.Chown(userCacheDir, uid, gid)
		os.Chown(userWsdlDir, uid, gid)
	}

	phpIni := fmt.Sprintf(`; PHP security settings for user: %s
; Isolation: Each user has their own temp/session/cache directories
; Generated by FastCP

[PHP]
; Security
open_basedir = %s
disable_functions = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec,proc_get_status,proc_terminate,proc_close,escapeshellcmd,escapeshellarg,show_source,posix_kill,posix_mkfifo,posix_getpwuid,posix_setpgid,posix_setsid,posix_setuid,posix_setgid,posix_seteuid,posix_setegid,posix_uname,php_uname,dl
expose_php = Off
display_errors = Off
display_startup_errors = Off
log_errors = On
error_log = /var/log/fastcp/php-%s-error.log
html_errors = Off

; Per-user temp directories (complete isolation from /tmp)
sys_temp_dir = %s
upload_tmp_dir = %s

[Session]
session.save_handler = files
session.save_path = %s
session.cookie_httponly = 1
session.cookie_secure = 0
session.use_strict_mode = 1

[opcache]
opcache.enable = 1
opcache.lockfile_path = %s

[soap]
soap.wsdl_cache_dir = %s
soap.wsdl_cache_enabled = 1

[Limits]
upload_max_filesize = 64M
post_max_size = 64M
max_execution_time = 300
memory_limit = 256M
max_input_vars = 5000

[Security Headers]
session.cookie_samesite = Strict
`, username, openBasedir, username, userTmpDir, userUploadDir, userSessionDir, userCacheDir, userWsdlDir)

	phpIniPath := filepath.Join(userConfigDir, "php.ini")
	os.WriteFile(phpIniPath, []byte(phpIni), 0644)

	buf.WriteString(`    handle {
        respond "Site not found" 404
    }
}

`)

	caddyfilePath := filepath.Join(userConfigDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write user Caddyfile: %w", err)
	}

	// Reload user's FrankenPHP service
	serviceName := fmt.Sprintf("fastcp-php@%s.service", username)
	exec.Command("systemctl", "reload-or-restart", serviceName).Run()

	return nil
}

type siteInfo struct {
	ID            string
	Domain        string
	Username      string
	DocumentRoot  string
	SafeDomain    string
	Domains       []siteDomainInfo
	PrimaryDomain string
	IsSuspended   bool
}

type siteDomainInfo struct {
	Domain            string
	IsPrimary         bool
	RedirectToPrimary bool
}

// Database handlers

func (s *Server) handleCreateDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	var req CreateDatabaseRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("creating database", "db_name", req.DBName, "db_user", req.DBUser)

	db, err := sql.Open("mysql", fmt.Sprintf("root@unix(%s)/", mysqlSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Create database
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", req.DBName))
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Create user for both localhost (socket) and 127.0.0.1 (TCP) -- MySQL treats them as different hosts
	for _, host := range []string{"localhost", "127.0.0.1"} {
		_, err = db.Exec(fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", req.DBUser, host, req.Password))
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		_, err = db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", req.DBName, req.DBUser, host))
		if err != nil {
			return nil, fmt.Errorf("failed to grant privileges: %w", err)
		}
	}

	_, err = db.Exec("FLUSH PRIVILEGES")
	if err != nil {
		return nil, fmt.Errorf("failed to flush privileges: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleDeleteDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	var req DeleteDatabaseRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("deleting database", "db_name", req.DBName, "db_user", req.DBUser)

	db, err := sql.Open("mysql", fmt.Sprintf("root@unix(%s)/", mysqlSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Drop user from both hosts
	db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", req.DBUser))
	db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'127.0.0.1'", req.DBUser))

	// Drop database
	_, err = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", req.DBName))
	if err != nil {
		return nil, fmt.Errorf("failed to drop database: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

// SSH handlers

func (s *Server) handleAddSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	var req AddSSHKeyRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("adding SSH key", "username", req.Username, "key_id", req.KeyID)

	// Get user info
	u, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Create .ssh directory
	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create .ssh directory: %w", err)
	}
	os.Chown(sshDir, uid, gid)

	// Append to authorized_keys
	authKeysPath := filepath.Join(sshDir, "authorized_keys")
	f, err := os.OpenFile(authKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open authorized_keys: %w", err)
	}
	defer f.Close()

	// Add key with FastCP marker
	keyLine := fmt.Sprintf("%s # fastcp:%s:%s\n", strings.TrimSpace(req.PublicKey), req.KeyID, req.Name)
	if _, err := f.WriteString(keyLine); err != nil {
		return nil, fmt.Errorf("failed to write key: %w", err)
	}

	os.Chown(authKeysPath, uid, gid)

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleRemoveSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	var req RemoveSSHKeyRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("removing SSH key", "username", req.Username, "key_id", req.KeyID)

	u, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	authKeysPath := filepath.Join(u.HomeDir, ".ssh", "authorized_keys")

	// Read current file
	content, err := os.ReadFile(authKeysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read authorized_keys: %w", err)
	}

	// Filter out the key
	var newLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	marker := fmt.Sprintf("fastcp:%s:", req.KeyID)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, marker) {
			newLines = append(newLines, line)
		}
	}

	// Write back
	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}

	if err := os.WriteFile(authKeysPath, []byte(newContent), 0600); err != nil {
		return nil, fmt.Errorf("failed to write authorized_keys: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

// System handlers

func (s *Server) handleSystemStatus(ctx context.Context, params json.RawMessage) (any, error) {
	hostname, _ := os.Hostname()

	// Get load average
	loadAvg := 0.0
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fmt.Sscanf(string(data), "%f", &loadAvg)
	}

	// Get memory info
	var memTotal, memAvail uint64
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(line, "MemTotal: %d kB", &memTotal)
				memTotal *= 1024
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(line, "MemAvailable: %d kB", &memAvail)
				memAvail *= 1024
			}
		}
	}

	// Get disk info
	var stat syscall.Statfs_t
	var diskTotal, diskUsed uint64
	if err := syscall.Statfs("/", &stat); err == nil {
		diskTotal = stat.Blocks * uint64(stat.Bsize)
		diskFree := stat.Bavail * uint64(stat.Bsize)
		diskUsed = diskTotal - diskFree
	}

	// Get uptime
	var uptime int64
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		var uptimeFloat float64
		fmt.Sscanf(string(data), "%f", &uptimeFloat)
		uptime = int64(uptimeFloat)
	}

	// Get PHP version
	phpVersion := ""
	if output, err := exec.Command("php", "-v").Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 {
			parts := strings.Fields(lines[0])
			if len(parts) >= 2 {
				phpVersion = parts[1]
			}
		}
	}

	// Get MySQL version
	mysqlVersion := ""
	if output, err := exec.Command("mysql", "--version").Output(); err == nil {
		parts := strings.Fields(string(output))
		for i, p := range parts {
			if p == "Ver" && i+1 < len(parts) {
				mysqlVersion = parts[i+1]
				break
			}
		}
	}

	return &SystemStatus{
		Hostname:     hostname,
		OS:           "Ubuntu",
		Uptime:       uptime,
		LoadAverage:  loadAvg,
		MemoryTotal:  memTotal,
		MemoryUsed:   memTotal - memAvail,
		DiskTotal:    diskTotal,
		DiskUsed:     diskUsed,
		PHPVersion:   phpVersion,
		MySQLVersion: mysqlVersion,
	}, nil
}

func (s *Server) handleSystemServices(ctx context.Context, params json.RawMessage) (any, error) {
	services := []string{"fastcp", "fastcp-agent", "mysql", "php-fpm"}
	var result []*ServiceStatus

	for _, svc := range services {
		status := &ServiceStatus{Name: svc, Status: "unknown", Enabled: false}

		// Check if active
		if err := exec.Command("systemctl", "is-active", "--quiet", svc).Run(); err == nil {
			status.Status = "running"
		} else {
			status.Status = "stopped"
		}

		// Check if enabled
		if err := exec.Command("systemctl", "is-enabled", "--quiet", svc).Run(); err == nil {
			status.Enabled = true
		}

		result = append(result, status)
	}

	return result, nil
}

func (s *Server) handleSystemUpdate(ctx context.Context, params json.RawMessage) (any, error) {
	var req PerformUpdateRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	version := req.TargetVersion
	if version == "" {
		version = "latest"
	}

	slog.Info("performing system update", "target_version", version)

	// Detect architecture
	arch := "amd64"
	cmd := exec.Command("uname", "-m")
	if output, err := cmd.Output(); err == nil {
		archStr := strings.TrimSpace(string(output))
		if archStr == "aarch64" || archStr == "arm64" {
			arch = "arm64"
		}
	}

	// Build download URLs
	var baseURL string
	if version == "latest" {
		baseURL = "https://github.com/rehmatworks/fastcp/releases/latest/download"
	} else {
		baseURL = fmt.Sprintf("https://github.com/rehmatworks/fastcp/releases/download/%s", version)
	}

	// Download new binaries to temp location
	tmpDir := "/tmp/fastcp-update"
	os.RemoveAll(tmpDir)
	os.MkdirAll(tmpDir, 0755)

	binaries := []struct {
		url  string
		dest string
	}{
		{fmt.Sprintf("%s/fastcp-linux-%s", baseURL, arch), "/opt/fastcp/bin/fastcp"},
		{fmt.Sprintf("%s/fastcp-agent-linux-%s", baseURL, arch), "/opt/fastcp/bin/fastcp-agent"},
	}

	for _, bin := range binaries {
		tmpPath := filepath.Join(tmpDir, filepath.Base(bin.dest))

		// Download with curl
		cmd := exec.Command("curl", "-fsSL", bin.url, "-o", tmpPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("failed to download %s: %w: %s", bin.url, err, output)
		}

		// Make executable
		os.Chmod(tmpPath, 0755)
	}

	// Stop services before replacing binaries
	exec.Command("systemctl", "stop", "fastcp").Run()

	// Replace binaries
	for _, bin := range binaries {
		tmpPath := filepath.Join(tmpDir, filepath.Base(bin.dest))

		// Backup old binary
		backupPath := bin.dest + ".bak"
		os.Rename(bin.dest, backupPath)

		// Move new binary into place
		if err := os.Rename(tmpPath, bin.dest); err != nil {
			// Restore backup on failure
			os.Rename(backupPath, bin.dest)
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("failed to install %s: %w", bin.dest, err)
		}

		// Remove backup
		os.Remove(backupPath)
	}

	// Cleanup
	os.RemoveAll(tmpDir)

	// Ensure configs are up-to-date before restarting
	s.runStartupMigrations()

	// Restart services
	exec.Command("systemctl", "start", "fastcp").Run()
	exec.Command("systemctl", "restart", "fastcp-agent").Run()

	slog.Info("system update completed", "version", version)
	return map[string]string{"status": "ok", "version": version}, nil
}

// User handlers

func (s *Server) handleCreateUser(ctx context.Context, params json.RawMessage) (any, error) {
	var req CreateUserRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("creating user", "username", req.Username, "memory_mb", req.MemoryMB, "cpu_percent", req.CPUPercent)

	// Create user with home directory
	cmd := exec.Command("useradd", "-m", "-s", "/bin/bash", req.Username)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create user: %w: %s", err, output)
	}

	// Set password
	cmd = exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", req.Username, req.Password))
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to set password: %w: %s", err, output)
	}

	// Create apps directory
	u, _ := user.Lookup(req.Username)
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	appsPath := filepath.Join(u.HomeDir, appsDir)
	os.MkdirAll(appsPath, 0755)
	os.Chown(appsPath, uid, gid)

	// Create .fastcp directory and runtime subdirectory
	fastcpPath := filepath.Join(u.HomeDir, fastcpDir)
	os.MkdirAll(fastcpPath, 0755)
	os.Chown(fastcpPath, uid, gid)

	runPath := filepath.Join(fastcpPath, "run")
	os.MkdirAll(runPath, 0755)
	os.Chown(runPath, uid, gid)

	// Create per-user FrankenPHP configuration directory
	userConfigDir := filepath.Join("/opt/fastcp/config/users", req.Username)
	os.MkdirAll(userConfigDir, 0755)

	// Create user's Caddyfile (initially empty, will be populated when sites are created)
	userCaddyfile := filepath.Join(userConfigDir, "Caddyfile")
	initialCaddyfile := fmt.Sprintf(`# FrankenPHP config for user: %s
{
    admin off
    
    frankenphp {
        num_threads 4
    }
}

# Sites will be added here by FastCP
`, req.Username)
	os.WriteFile(userCaddyfile, []byte(initialCaddyfile), 0644)

	// Create systemd user slice with resource limits (applies to ALL user processes)
	if err := s.createUserResourceSlice(req.Username, uid, req.MemoryMB, req.CPUPercent); err != nil {
		slog.Warn("failed to create user resource slice", "error", err)
	}

	// Create systemd service for this user's FrankenPHP instance
	if err := s.createUserPHPService(req.Username); err != nil {
		slog.Warn("failed to create PHP service", "error", err)
	}

	return map[string]string{"status": "ok"}, nil
}

// createUserResourceSlice creates a systemd slice override for the user to limit ALL their processes
func (s *Server) createUserResourceSlice(username string, uid, memoryMB, cpuPercent int) error {
	// Default values
	if memoryMB == 0 {
		memoryMB = 512
	}
	if cpuPercent == 0 {
		cpuPercent = 100
	}

	// Create override directory for user-UID.slice
	sliceDir := fmt.Sprintf("/etc/systemd/system/user-%d.slice.d", uid)
	if err := os.MkdirAll(sliceDir, 0755); err != nil {
		return fmt.Errorf("failed to create slice directory: %w", err)
	}

	// Create override file with resource limits
	overrideContent := fmt.Sprintf(`# FastCP resource limits for user: %s (UID: %d)
# These limits apply to ALL processes by this user:
# - FrankenPHP (web server)
# - SSH sessions
# - Cron jobs
# - Any other processes

[Slice]
MemoryMax=%dM
CPUQuota=%d%%
`, username, uid, memoryMB, cpuPercent)

	overridePath := filepath.Join(sliceDir, "50-fastcp-limits.conf")
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		return fmt.Errorf("failed to write slice override: %w", err)
	}

	// Reload systemd to apply changes
	exec.Command("systemctl", "daemon-reload").Run()

	slog.Info("created user resource slice", "username", username, "uid", uid, "memory_mb", memoryMB, "cpu_percent", cpuPercent)
	return nil
}

func (s *Server) createUserPHPService(username string) error {
	serviceName := fmt.Sprintf("fastcp-php@%s.service", username)
	servicePath := fmt.Sprintf("/etc/systemd/system/%s", serviceName)

	// Verify user exists
	if _, err := user.Lookup(username); err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// Note: Resource limits (CPU/RAM) are enforced at the user-UID.slice level,
	// which applies to ALL user processes including this service, SSH sessions, and cron jobs
	serviceContent := fmt.Sprintf(`[Unit]
Description=FastCP PHP Service for %s
After=network.target

[Service]
Type=simple
User=%s
Group=%s
Environment=HOME=/home/%s
Environment=PHP_INI_SCAN_DIR=/opt/fastcp/config/users/%s
ExecStart=/usr/local/bin/frankenphp run --config /opt/fastcp/config/users/%s/Caddyfile
Restart=always
RestartSec=5

# Security
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/home/%s /var/log/fastcp /tmp

[Install]
WantedBy=multi-user.target
`, username, username, username, username, username, username, username)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd
	exec.Command("systemctl", "daemon-reload").Run()

	// Enable the service (but don't start yet - no sites)
	exec.Command("systemctl", "enable", serviceName).Run()

	slog.Info("created PHP service for user", "username", username)
	return nil
}

func (s *Server) handleDeleteUser(ctx context.Context, params json.RawMessage) (any, error) {
	var req DeleteUserRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("deleting user", "username", req.Username)

	// Get user's UID before deletion (needed for slice cleanup)
	var uid int
	if u, err := user.Lookup(req.Username); err == nil {
		uid, _ = strconv.Atoi(u.Uid)
	}

	// Stop and disable the user's PHP service
	serviceName := fmt.Sprintf("fastcp-php@%s.service", req.Username)
	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()
	os.Remove(fmt.Sprintf("/etc/systemd/system/%s", serviceName))

	// Remove user resource slice overrides
	if uid > 0 {
		sliceDir := fmt.Sprintf("/etc/systemd/system/user-%d.slice.d", uid)
		os.RemoveAll(sliceDir)
	}

	exec.Command("systemctl", "daemon-reload").Run()

	// Delete all MySQL databases owned by this user
	if err := s.deleteUserDatabases(req.Username); err != nil {
		slog.Warn("failed to delete user databases", "username", req.Username, "error", err)
	}

	// Remove user's config directory
	os.RemoveAll(filepath.Join("/opt/fastcp/config/users", req.Username))

	// Delete user and home directory
	cmd := exec.Command("userdel", "-r", req.Username)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to delete user: %w: %s", err, output)
	}

	// Reload Caddy to remove site configurations
	if err := s.generateCaddyfile(); err != nil {
		slog.Warn("failed to regenerate Caddyfile", "error", err)
	}
	exec.Command("pkill", "-USR1", "frankenphp").Run()

	return map[string]string{"status": "ok"}, nil
}

// deleteUserDatabases drops all MySQL databases and users owned by a system user
func (s *Server) deleteUserDatabases(username string) error {
	// Open FastCP database to get user's databases
	db, err := sql.Open("sqlite3", "/opt/fastcp/data/fastcp.db")
	if err != nil {
		return fmt.Errorf("failed to open FastCP database: %w", err)
	}
	defer db.Close()

	// Get all databases for this user
	rows, err := db.Query("SELECT db_name, db_user FROM databases WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var databases []struct {
		DBName string
		DBUser string
	}
	for rows.Next() {
		var d struct {
			DBName string
			DBUser string
		}
		if err := rows.Scan(&d.DBName, &d.DBUser); err != nil {
			continue
		}
		databases = append(databases, d)
	}

	if len(databases) == 0 {
		return nil
	}

	// Connect to MySQL
	mysqlDB, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer mysqlDB.Close()

	// Drop each database and user
	for _, d := range databases {
		slog.Info("dropping database", "database", d.DBName, "user", d.DBUser)

		// Drop database
		if _, err := mysqlDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", d.DBName)); err != nil {
			slog.Warn("failed to drop database", "database", d.DBName, "error", err)
		}

		// Drop user (try both localhost and 127.0.0.1)
		mysqlDB.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", d.DBUser))
		mysqlDB.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'127.0.0.1'", d.DBUser))
	}

	mysqlDB.Exec("FLUSH PRIVILEGES")
	return nil
}

// Helper functions

func chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, uid, gid)
	})
}

func setACLs(path, username string) error {
	// Set ACLs to restrict access to owner only
	cmds := [][]string{
		{"setfacl", "-R", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		{"setfacl", "-R", "-d", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		{"setfacl", "-R", "-m", "o::---", path},
	}

	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("setfacl failed: %w", err)
		}
	}

	return nil
}

// Cron handlers

func (s *Server) handleSyncCronJobs(ctx context.Context, params json.RawMessage) (any, error) {
	var req SyncCronJobsRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("syncing cron jobs", "username", req.Username, "count", len(req.Jobs))

	// Verify user exists
	if _, err := user.Lookup(req.Username); err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	// Build crontab content
	var lines []string
	lines = append(lines, "# FastCP managed cron jobs - DO NOT EDIT MANUALLY")
	lines = append(lines, "# Changes will be overwritten by FastCP")
	lines = append(lines, "")

	for _, job := range req.Jobs {
		if !job.Enabled {
			continue
		}
		lines = append(lines, fmt.Sprintf("# %s (ID: %s)", job.Name, job.ID))
		lines = append(lines, fmt.Sprintf("%s %s", job.Expression, job.Command))
		lines = append(lines, "")
	}

	crontabContent := strings.Join(lines, "\n")

	// Write crontab using crontab command
	cmd := exec.Command("crontab", "-u", req.Username, "-")
	cmd.Stdin = strings.NewReader(crontabContent)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to update crontab: %w: %s", err, output)
	}

	slog.Info("cron jobs synced", "username", req.Username)
	return map[string]string{"status": "ok"}, nil
}
