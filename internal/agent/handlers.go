//go:build linux

package agent

import (
	"bufio"
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
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
	fastcpMotd    = "/etc/update-motd.d/99-fastcp"
)

// runStartupMigrations fixes configuration drift on agent startup (e.g. after updates).
func (s *Server) runStartupMigrations() {
	s.ensureRunDir()
	s.ensureMOTDWelcome()
	s.ensureCaddyBinary()
	s.ensureBackupDependencies()
	s.ensurePHPIniConfig()
	s.ensureServiceFiles()
	s.ensurePMAConfig()
	s.ensureMySQLTuning()
	s.ensureSwap()
	s.bootstrapAllUsers()
	s.cleanStaleSocketsAndReload()
}

func (s *Server) installMissingBackupDependencies() error {
	pkgByBinary := map[string]string{
		"restic": "restic",
		"rsync":  "rsync",
	}
	missingPkgs := make([]string, 0, len(pkgByBinary))
	for bin, pkg := range pkgByBinary {
		if _, err := exec.LookPath(bin); err != nil {
			missingPkgs = append(missingPkgs, pkg)
		}
	}
	if len(missingPkgs) == 0 {
		return nil
	}
	sort.Strings(missingPkgs)
	if output, err := runAptCommand("update", "-qq"); err != nil {
		return fmt.Errorf("failed to update apt indexes for backup dependencies: %w: %s", err, strings.TrimSpace(string(output)))
	}
	args := append([]string{"install", "-y", "-qq"}, missingPkgs...)
	if output, err := runAptCommand(args...); err != nil {
		return fmt.Errorf("failed to install backup dependencies (%s): %w: %s", strings.Join(missingPkgs, ", "), err, strings.TrimSpace(string(output)))
	}
	slog.Info("installed missing backup dependencies", "packages", strings.Join(missingPkgs, ","))
	return nil
}

func (s *Server) ensureBackupDependencies() {
	if err := s.installMissingBackupDependencies(); err != nil {
		slog.Warn("failed to ensure backup dependencies", "error", err)
	}
}

func (s *Server) ensureRunDir() {
	os.MkdirAll(fastcpRunDir, 0755)

	// Clean up old tmpfs-based runtime dir
	os.Remove("/etc/tmpfiles.d/fastcp.conf")
}

func (s *Server) ensureMOTDWelcome() {
	content := `#!/bin/sh
#
# FastCP login welcome message (MOTD)
#

host_name="$(hostname 2>/dev/null)"
host_ips="$(hostname -I 2>/dev/null || true)"
primary_ipv4="$(echo "$host_ips" | tr ' ' '\n' | awk '/^([0-9]{1,3}\.){3}[0-9]{1,3}$/ {print; exit}')"
primary_ipv6="$(echo "$host_ips" | tr ' ' '\n' | awk 'index($0, ":") > 0 {gsub(/%.*/, "", $0); print; exit}')"
if [ -n "$primary_ipv4" ]; then
  panel_host="$primary_ipv4"
elif [ -n "$primary_ipv6" ]; then
  panel_host="[$primary_ipv6]"
else
  panel_host="127.0.0.1"
fi

uptime_human="$(uptime -p 2>/dev/null | sed 's/^up //')"
[ -z "$uptime_human" ] && uptime_human="$(uptime 2>/dev/null | sed 's/.*up \([^,]*\),.*/\1/')"
[ -z "$uptime_human" ] && uptime_human="N/A"

load_avg="$(awk '{print $1" "$2" "$3}' /proc/loadavg 2>/dev/null)"
[ -z "$load_avg" ] && load_avg="N/A"

mem_total_mb="$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo 2>/dev/null)"
mem_avail_mb="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo 2>/dev/null)"
if [ -n "$mem_total_mb" ] && [ -n "$mem_avail_mb" ]; then
  mem_used_mb=$((mem_total_mb - mem_avail_mb))
  memory_line="${mem_used_mb}MB / ${mem_total_mb}MB"
else
  memory_line="N/A"
fi

disk_root="$(df -h / 2>/dev/null | awk 'NR==2 {print $3" / "$2" ("$5" used)"}')"
[ -z "$disk_root" ] && disk_root="N/A"

fastcp_version=""
if [ -x /opt/fastcp/bin/fastcp ]; then
  fastcp_version="$(/opt/fastcp/bin/fastcp --version 2>/dev/null | awk '{print $2}')"
  case "$fastcp_version" in
    ""|dev|unknown) fastcp_version="" ;;
  esac
fi

panel_url="https://${panel_host}:2050"

service_line=""
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  panel_status="$(systemctl is-active fastcp 2>/dev/null || echo unknown)"
  agent_status="$(systemctl is-active fastcp-agent 2>/dev/null || echo unknown)"
  service_line="fastcp=${panel_status}, agent=${agent_status}"
fi

printf "\n"
printf "FastCP Server Welcome\n"
printf "---------------------\n"
printf "Control Panel: %s\n" "$panel_url"
printf "Host: %s\n" "${host_name:-N/A}"
printf "Uptime: %s\n" "$uptime_human"
printf "Load Average: %s\n" "$load_avg"
printf "Memory Usage: %s\n" "$memory_line"
printf "Disk Usage (/): %s\n" "$disk_root"
[ -n "$service_line" ] && printf "Services: %s\n" "$service_line"
[ -n "$fastcp_version" ] && printf "FastCP Version: %s\n" "$fastcp_version"
printf "Docs: https://fastcp.org/docs\n"
printf "GitHub: https://github.com/rehmatworks/fastcp\n"
printf "\n"
`

	existing, err := os.ReadFile(fastcpMotd)
	if err == nil && string(existing) == content {
		return
	}

	if err := os.WriteFile(fastcpMotd, []byte(content), 0755); err != nil {
		slog.Warn("failed to write FastCP MOTD script", "path", fastcpMotd, "error", err)
		return
	}
	if err := os.Chmod(fastcpMotd, 0755); err != nil {
		slog.Warn("failed to set executable bit on FastCP MOTD script", "path", fastcpMotd, "error", err)
	}
}

func (s *Server) ensureCaddyBinary() {
	caddyPath := "/usr/local/bin/caddy"
	if _, err := os.Stat(caddyPath); err == nil {
		return
	}

	slog.Info("downloading plain Caddy binary")
	arch := "amd64"
	if output, err := exec.Command("uname", "-m").Output(); err == nil {
		m := strings.TrimSpace(string(output))
		if m == "aarch64" || m == "arm64" {
			arch = "arm64"
		}
	}

	downloadURL := fmt.Sprintf("https://caddyserver.com/api/download?os=linux&arch=%s", arch)
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0755); err != nil {
		slog.Error("failed to create caddy directory", "error", err)
		return
	}
	tmpFile, err := os.CreateTemp("/tmp", "fastcp-caddy-*")
	if err != nil {
		slog.Error("failed to create caddy temp file", "error", err)
		return
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	if output, err := exec.Command("curl", "-fsSL", "-o", tmpPath, downloadURL).CombinedOutput(); err != nil {
		slog.Error("failed to download Caddy", "error", err, "output", string(output))
		return
	}
	fi, statErr := os.Stat(tmpPath)
	if statErr != nil || fi.Size() == 0 {
		slog.Error("downloaded Caddy binary is empty", "error", statErr)
		return
	}
	if output, err := exec.Command("install", "-m", "0755", tmpPath, caddyPath).CombinedOutput(); err != nil {
		slog.Error("failed to install Caddy binary", "error", err, "output", string(output))
		return
	}
	slog.Info("installed plain Caddy binary", "path", caddyPath)

	if s.hasSystemd() {
		_ = s.serviceReloadOrRestart("fastcp-caddy")
	}
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

	// Ensure fastcp-caddy.service has caddy reload support
	caddyUnit := "/etc/systemd/system/fastcp-caddy.service"
	if data, err := os.ReadFile(caddyUnit); err == nil {
		content := string(data)
		changed := false
		if strings.Contains(content, "Environment=PHP_INI_SCAN_DIR=:/opt/fastcp/config/php\n") {
			content = strings.ReplaceAll(content, "Environment=PHP_INI_SCAN_DIR=:/opt/fastcp/config/php\n", "")
			changed = true
		}
		if !strings.Contains(content, "ExecReload=/usr/local/bin/caddy reload --config /opt/fastcp/config/Caddyfile") {
			content = strings.Replace(content, "RestartSec=5\n", "RestartSec=5\nExecReload=/usr/local/bin/caddy reload --config /opt/fastcp/config/Caddyfile\n", 1)
			changed = true
		}
		if changed {
			os.WriteFile(caddyUnit, []byte(content), 0644)
			needsReload = true
			slog.Info("updated fastcp-caddy.service settings")
		}
	}

	// Remove legacy fastcp-php@ user service files
	phpUnits, _ := filepath.Glob("/etc/systemd/system/fastcp-php@*.service")
	for _, phpUnit := range phpUnits {
		unit := filepath.Base(phpUnit)
		if s.hasSystemd() {
			_ = s.runSystemctl("disable", "--now", unit)
		}
		os.Remove(phpUnit)
		needsReload = true
		slog.Info("removed legacy per-user php service unit", "unit", unit)
	}

	if needsReload && s.hasSystemd() {
		_ = s.runSystemctl("daemon-reload")
	}
}

// bootstrapAllUsers ensures every user that has a config directory also has
// all required runtime directories with correct ownership. Also migrates
// away from the legacy shared-socket layout if still present.
func (s *Server) bootstrapAllUsers() {
	// Migrate away from old shared /opt/fastcp/run/php-*.sock layout
	oldSockets, _ := filepath.Glob(filepath.Join(fastcpRunDir, "php-*.sock"))
	if len(oldSockets) > 0 {
		slog.Info("migrating legacy shared sockets to per-user home directories")
		for _, sock := range oldSockets {
			os.Remove(sock)
		}
		oldPids, _ := filepath.Glob(filepath.Join(fastcpRunDir, "php-*.pid"))
		for _, pid := range oldPids {
			os.Remove(pid)
		}
		time.Sleep(1 * time.Second)
	}

	userDirs, _ := filepath.Glob("/opt/fastcp/config/users/*")
	for _, dir := range userDirs {
		bootstrapUserEnvironment(filepath.Base(dir))
	}
}

func (s *Server) cleanStaleSocketsAndReload() {
	// After a reboot, socket files persist on disk but the processes are dead.
	// Remove stale sockets, then regenerate and reload.
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
		if !s.isCaddyRunning() {
			if err := s.startCaddy(); err != nil {
				slog.Warn("failed to start Caddy on startup", "error", err)
			}
		} else if err := s.reloadCaddy(); err != nil {
			slog.Warn("failed to reload Caddy on startup", "error", err)
		}
	}
}

func (s *Server) ensureMySQLTuning() {
	cnfPath := "/etc/mysql/conf.d/fastcp.cnf"
	if _, err := os.Stat(cnfPath); err == nil {
		return
	}

	// Conservative defaults for predictable low-resource baseline behavior.
	bufferPool, maxConn, perfSchema := 128, 30, "OFF"

	cnf := fmt.Sprintf(`[mysqld]
# FastCP tuning (default low-resource profile)
innodb_buffer_pool_size = %dM
innodb_log_file_size = 16M
innodb_log_buffer_size = 8M
innodb_flush_log_at_trx_commit = 2
innodb_flush_method = O_DIRECT
key_buffer_size = 4M
max_connections = %d
table_open_cache = 200
thread_cache_size = 8
performance_schema = %s
skip-name-resolve
`, bufferPool, maxConn, perfSchema)

	os.MkdirAll("/etc/mysql/conf.d", 0755)
	if err := os.WriteFile(cnfPath, []byte(cnf), 0644); err == nil {
		_ = s.restartMySQLService()
		slog.Info("applied MySQL tuning", "profile", "low_resource_default", "buffer_pool_mb", bufferPool, "max_connections", maxConn, "perf_schema", perfSchema)
	}
}

func (s *Server) ensureSwap() {
	// Only add swap on servers with <= 2GB RAM and insufficient existing swap
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}

	var totalKB, swapKB int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &totalKB)
		} else if strings.HasPrefix(line, "SwapTotal:") {
			fmt.Sscanf(line, "SwapTotal: %d", &swapKB)
		}
	}

	if totalKB > 2*1024*1024 || swapKB >= 512*1024 {
		return
	}

	if _, err := os.Stat("/swapfile"); err != nil {
		cmd := exec.Command("fallocate", "-l", "1G", "/swapfile")
		if cmd.Run() != nil {
			return
		}
		os.Chmod("/swapfile", 0600)
		exec.Command("mkswap", "/swapfile").Run()
	}
	exec.Command("swapon", "/swapfile").Run()

	// Ensure it's in fstab
	if fstab, err := os.ReadFile("/etc/fstab"); err == nil {
		if !strings.Contains(string(fstab), "/swapfile") {
			f, _ := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString("\n/swapfile none swap sw 0 0\n")
				f.Close()
			}
		}
	}
	slog.Info("ensured swap is active", "ram_kb", totalKB, "swap_kb", swapKB)
}

func (s *Server) ensurePMAConfig() {
	configFile := "/opt/fastcp/phpmyadmin/config.inc.php"
	blowfish := ""
	if data, err := os.ReadFile(configFile); err == nil {
		secretRE := regexp.MustCompile(`\$cfg\['blowfish_secret'\]\s*=\s*'([^']*)';`)
		for _, line := range strings.Split(string(data), "\n") {
			m := secretRE.FindStringSubmatch(line)
			if len(m) == 2 {
				blowfish = m[1]
				break
			}
		}
	}
	if blowfish == "" {
		buf := make([]byte, 32)
		if _, err := cryptoRand.Read(buf); err == nil {
			blowfish = fmt.Sprintf("%x", buf)
		} else {
			blowfish = "fastcp-default-blowfish-secret-change-me"
		}
	}

	content := fmt.Sprintf(`<?php
$cfg['blowfish_secret'] = '%s';
$cfg['TempDir'] = '/opt/fastcp/run/phpmyadmin-tmp';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';

$cfg['Servers'][1]['host'] = '127.0.0.1';
$cfg['Servers'][1]['auth_type'] = 'config';
$cfg['Servers'][1]['user'] = $_SERVER['PHP_AUTH_USER'] ?? '';
$cfg['Servers'][1]['password'] = $_SERVER['PHP_AUTH_PW'] ?? '';
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['Servers'][1]['hide_db'] = '^(information_schema|performance_schema|mysql|sys)$';

$cfg['ShowCreateDb'] = false;
$cfg['LoginCookieValidity'] = 3600;
$cfg['LoginCookieStore'] = 0;
$cfg['LoginCookieDeleteAll'] = true;
`, blowfish)

	if err := os.WriteFile(configFile, []byte(content), 0644); err == nil {
		// Remove any leftover legacy signon entrypoint.
		os.Remove("/opt/fastcp/phpmyadmin/signon.php")
		slog.Info("wrote phpMyAdmin config (config auth only)")
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
	if err := copyDefaultSiteLogo(filepath.Join(siteDir, "public")); err != nil {
		slog.Warn("failed to copy default site logo", "error", err)
	}

	// Create default index.php with minimalist welcome page
	indexPath := filepath.Join(siteDir, "public", "index.php")
	indexContent := fmt.Sprintf(`<?php
$domain = '%s';
$docRoot = '%s';
$logoPath = '';
$logoCandidates = ['/.fastcp/logo.svg', '/.fastcp/logo-small.png', '/.fastcp/icon.png'];
foreach ($logoCandidates as $candidate) {
    $full = __DIR__ . $candidate;
    if (!is_file($full)) {
        continue;
    }
    // Skip malformed svg files from old templates.
    if (str_ends_with($candidate, '.svg')) {
        $snippet = @file_get_contents($full, false, null, 0, 2048);
        if ($snippet === false || strpos($snippet, '%%') !== false) {
            continue;
        }
    }
    if (is_file($full)) {
        $logoPath = $candidate;
        break;
    }
}
?>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title><?php echo htmlspecialchars($domain, ENT_QUOTES, 'UTF-8'); ?> - FastCP</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #ffffff;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 32px 18px;
            color: #1f2937;
        }
        .shell {
            max-width: 760px;
            width: 100%%;
            border: 1px solid #e5e7eb;
            border-radius: 18px;
            padding: 42px 34px;
            box-shadow: 0 16px 36px rgba(2, 8, 23, 0.06);
            background: #fff;
        }
        .brand {
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 78px;
            margin: 0 auto 24px;
        }
        .brand img {
            display: block;
            max-width: min(320px, 92%%);
            max-height: 78px;
            width: auto;
            height: auto;
            object-fit: contain;
            object-position: center;
            vertical-align: middle;
        }
        h1 {
            text-align: center;
            font-size: 34px;
            font-weight: 700;
            color: #0f172a;
            letter-spacing: -0.02em;
            margin-bottom: 10px;
        }
        .domain {
            text-align: center;
            font-size: 15px;
            color: #004aad;
            font-weight: 500;
            margin-bottom: 20px;
        }
        .status {
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 8px 16px;
            background: #f8fafc;
            border: 1px solid #e2e8f0;
            border-radius: 100px;
            color: #0f172a;
            font-size: 14px;
            font-weight: 500;
            margin: 0 auto 24px;
            width: max-content;
        }
        .status::before {
            content: '';
            width: 8px;
            height: 8px;
            background: #16a34a;
            border-radius: 50%%;
            margin-right: 8px;
        }
        .message {
            text-align: center;
            color: #4b5563;
            font-size: 15px;
            line-height: 1.7;
            margin: 0 auto 20px;
        }
        .pathline {
            display: inline-block;
            margin-top: 8px;
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
            color: #111827;
            font-size: 12px;
            font-weight: 600;
            word-break: break-all;
        }
        .footer {
            text-align: center;
            font-size: 13px;
            color: #6b7280;
            line-height: 1.6;
        }
        .footer a {
            color: #004aad;
            text-decoration: none;
            font-weight: 500;
        }
        .footer a:hover { text-decoration: underline; }
        @media (max-width: 720px) {
            .shell { padding: 28px 20px; }
            h1 { font-size: 28px; }
        }
    </style>
</head>
<body>
    <main class="shell">
        <?php if ($logoPath !== ''): ?>
        <div class="brand">
            <img src="<?php echo htmlspecialchars($logoPath, ENT_QUOTES, 'UTF-8'); ?>" alt="FastCP logo">
        </div>
        <?php endif; ?>

        <h1>Your website is ready</h1>
        <p class="domain"><?php echo $domain; ?></p>
        <div class="status">Live on FastCP</div>
        <p class="message">Upload your website files to the web public path:<br><span class="pathline"><?php echo $docRoot; ?></span></p>

        <p class="footer">
            Built with <a href="https://github.com/rehmatworks/fastcp" target="_blank" rel="noopener noreferrer">FastCP</a>
        </p>
    </main>
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

func copyDefaultSiteLogo(publicDir string) error {
	targetDir := filepath.Join(publicDir, ".fastcp")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	baseDirs := []string{
		"/opt/fastcp/ui/dist/assets",
		"/app/cmd/fastcp/ui/dist/assets",
		filepath.Join(".", "cmd", "fastcp", "ui", "dist", "assets"),
	}
	assetNames := []string{"logo.svg", "logo-small.png", "icon.png"}
	copied := false
	for _, baseDir := range baseDirs {
		for _, name := range assetNames {
			src := filepath.Join(baseDir, name)
			if _, err := os.Stat(src); err != nil {
				continue
			}
			in, err := os.Open(src)
			if err != nil {
				return err
			}
			dst := filepath.Join(targetDir, name)
			out, err := os.Create(dst)
			if err != nil {
				_ = in.Close()
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				_ = out.Close()
				_ = in.Close()
				return err
			}
			_ = out.Close()
			_ = in.Close()
			if err := os.Chmod(dst, 0644); err != nil {
				return err
			}
			copied = true
		}
	}
	if copied {
		return nil
	}
	targetPath := filepath.Join(targetDir, "logo.svg")
	fallback := `<svg xmlns="http://www.w3.org/2000/svg" width="420" height="90" viewBox="0 0 420 90" fill="none"><text x="50%" y="56%" text-anchor="middle" dominant-baseline="middle" font-family="Inter, Arial, sans-serif" font-size="44" font-weight="700" fill="#004AAD">FastCP</text></svg>`
	if err := os.WriteFile(targetPath, []byte(fallback), 0644); err != nil {
		return err
	}
	return nil
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
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	slog.Info("regenerating and reloading Caddy configuration")

	if err := s.generateCaddyfile(); err != nil {
		return nil, fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	if !s.isCaddyRunning() {
		slog.Info("Caddy not running, starting it")
		if err := s.startCaddy(); err != nil {
			return nil, fmt.Errorf("failed to start Caddy: %w", err)
		}
		return map[string]string{"status": "ok", "action": "started"}, nil
	}

	if err := s.reloadCaddy(); err != nil {
		return nil, fmt.Errorf("failed to reload Caddy: %w", err)
	}
	return map[string]string{"status": "ok", "action": "reloaded"}, nil
}

func (s *Server) isCaddyRunning() bool {
	if s.hasSystemd() {
		return exec.Command("systemctl", "is-active", "--quiet", "fastcp-caddy").Run() == nil
	}
	return len(s.runningCaddyPIDs()) > 0
}

func (s *Server) runningCaddyPIDs() []string {
	out, err := exec.Command("ps", "-C", "caddy", "-o", "stat=").Output()
	if err != nil {
		return nil
	}
	pidOut, err := exec.Command("ps", "-C", "caddy", "-o", "pid=").Output()
	if err != nil {
		return nil
	}
	var states []string
	for _, line := range strings.Split(string(out), "\n") {
		state := strings.TrimSpace(line)
		if state != "" {
			states = append(states, state)
		}
	}
	var pids []string
	for _, line := range strings.Split(string(pidOut), "\n") {
		pid := strings.TrimSpace(line)
		if pid != "" {
			pids = append(pids, pid)
		}
	}
	if len(states) != len(pids) {
		// Defensive: if process table output is inconsistent, prefer restart path.
		return nil
	}
	var running []string
	for i, state := range states {
		if !strings.HasPrefix(state, "Z") {
			running = append(running, pids[i])
		}
	}
	return running
}

func (s *Server) ensureSingleCaddyInstance() error {
	if s.hasSystemd() {
		return nil
	}
	pids := s.runningCaddyPIDs()
	if len(pids) <= 1 {
		return nil
	}
	slog.Warn("multiple caddy processes detected; forcing clean restart", "pids", strings.Join(pids, ","))
	if err := s.startCaddy(); err != nil {
		return fmt.Errorf("failed to recover from duplicate caddy processes: %w", err)
	}
	return nil
}

func (s *Server) startCaddy() error {
	if s.hasSystemd() {
		if err := s.serviceReloadOrRestart("fastcp-caddy"); err != nil {
			return fmt.Errorf("failed to restart caddy service: %w", err)
		}
		slog.Info("Caddy restarted via systemd")
		return nil
	}

	exec.Command("pkill", "-9", "caddy").Run()
	time.Sleep(1 * time.Second)

	cmd := exec.Command("/usr/local/bin/caddy", "run", "--config", caddyConfig)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Caddy: %w", err)
	}
	go cmd.Wait()
	time.Sleep(2 * time.Second)
	slog.Info("Caddy started", "pid", cmd.Process.Pid)
	return nil
}

func (s *Server) reloadCaddy() error {
	if s.hasSystemd() {
		return s.serviceReload("fastcp-caddy")
	}

	if err := s.ensureSingleCaddyInstance(); err != nil {
		return err
	}

	if output, err := exec.Command("/usr/local/bin/caddy", "reload", "--config", caddyConfig).CombinedOutput(); err != nil {
		return fmt.Errorf("caddy reload failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Server) hasSystemd() bool {
	// Detect systemd presence by runtime directory + systemctl availability.
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func (s *Server) runSystemctl(args ...string) error {
	if !s.hasSystemd() {
		return fmt.Errorf("systemd unavailable")
	}
	output, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Server) serviceReloadOrRestart(service string) error {
	if err := s.runSystemctl("reload-or-restart", service); err == nil {
		return nil
	}
	if err := s.runSystemctl("restart", service); err == nil {
		return nil
	}
	return s.runSystemctl("start", service)
}

func (s *Server) serviceReload(service string) error {
	if err := s.runSystemctl("reload", service); err == nil {
		return nil
	}
	if err := s.runSystemctl("restart", service); err == nil {
		return nil
	}
	return s.runSystemctl("start", service)
}

func userSocketDir(username string) string {
	return filepath.Join(homeBase, username, fastcpDir, "run")
}

func userSocketPath(username string) string {
	return filepath.Join(userSocketDir(username), "php.sock")
}

// bootstrapUserEnvironment creates all required directories and fixes
// ownership for a system user. Safe to call repeatedly (idempotent).
func bootstrapUserEnvironment(username string) {
	u, err := user.Lookup(username)
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	own := func(p string) { os.Chown(p, uid, gid) }
	mkown := func(p string, mode os.FileMode) {
		os.MkdirAll(p, mode)
		own(p)
	}

	// ~/apps/
	mkown(filepath.Join(u.HomeDir, appsDir), 0755)

	// ~/.fastcp/ and ~/.fastcp/run/ (socket directory)
	fastcpPath := filepath.Join(u.HomeDir, fastcpDir)
	mkown(fastcpPath, 0755)
	mkown(filepath.Join(fastcpPath, "run"), 0755)

	// ~/.tmp/ tree (sessions, uploads, cache, phpmyadmin, wsdl)
	tmpDir := filepath.Join(u.HomeDir, ".tmp")
	for _, sub := range []string{"", "sessions", "uploads", "cache", "phpmyadmin", "wsdl"} {
		mkown(filepath.Join(tmpDir, sub), 0700)
	}

	// /opt/fastcp/config/users/{username}/
	userConfigDir := filepath.Join("/opt/fastcp/config/users", username)
	os.MkdirAll(userConfigDir, 0755)

	// PHP error log
	logPath := fmt.Sprintf("/var/log/fastcp/php-%s-error.log", username)
	if _, err := os.Stat(logPath); err != nil {
		os.WriteFile(logPath, nil, 0644)
	}
	own(logPath)
}

func normalizePHPVersion(version string) string {
	v := strings.TrimSpace(version)
	if matched, _ := regexp.MatchString(`^\d+\.\d+$`, v); !matched {
		return ""
	}
	return v
}

func detectAvailablePHPVersions() []string {
	services := detectInstalledPHPFPMServices()
	matches, _ := filepath.Glob("/etc/php/*/fpm/php-fpm.conf")
	available := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, confPath := range matches {
		version := normalizePHPVersion(filepath.Base(filepath.Dir(filepath.Dir(confPath))))
		if version == "" {
			continue
		}
		if _, ok := seen[version]; ok {
			continue
		}
		hasBinary := false
		for _, candidate := range phpFPMBinaryCandidates(version) {
			if strings.HasPrefix(candidate, "/") {
				if _, err := os.Stat(candidate); err == nil {
					hasBinary = true
					break
				}
				continue
			}
			if _, err := exec.LookPath(candidate); err == nil {
				hasBinary = true
				break
			}
		}
		if !hasBinary {
			if _, ok := services[version]; !ok {
				continue
			}
		}
		seen[version] = struct{}{}
		available = append(available, version)
	}
	sort.Slice(available, func(i, j int) bool {
		majI, minI := 0, 0
		majJ, minJ := 0, 0
		fmt.Sscanf(available[i], "%d.%d", &majI, &minI)
		fmt.Sscanf(available[j], "%d.%d", &majJ, &minJ)
		if majI != majJ {
			return majI < majJ
		}
		return minI < minJ
	})
	return available
}

func phpFPMBinaryCandidates(version string) []string {
	return []string{
		fmt.Sprintf("php-fpm%s", version),
		fmt.Sprintf("php%s-fpm", version),
		fmt.Sprintf("/usr/sbin/php-fpm%s", version),
		fmt.Sprintf("/usr/sbin/php%s-fpm", version),
		fmt.Sprintf("/usr/local/sbin/php-fpm%s", version),
		fmt.Sprintf("/usr/local/sbin/php%s-fpm", version),
	}
}

func detectInstalledPHPFPMServices() map[string]string {
	serviceByVersion := map[string]string{}
	re := regexp.MustCompile(`^php(\d+\.\d+)-fpm\.service$`)
	dirs := []string{
		"/etc/systemd/system",
		"/lib/systemd/system",
		"/usr/lib/systemd/system",
	}
	for _, dir := range dirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "php*-fpm.service"))
		for _, path := range matches {
			base := filepath.Base(path)
			sub := re.FindStringSubmatch(base)
			if len(sub) != 2 {
				continue
			}
			version := normalizePHPVersion(sub[1])
			if version == "" {
				continue
			}
			serviceByVersion[version] = strings.TrimSuffix(base, ".service")
		}
	}
	return serviceByVersion
}

func resolvePHPFPMServiceName(version string) (string, error) {
	normalized := normalizePHPVersion(version)
	if normalized == "" {
		return "", fmt.Errorf("invalid php version %q", version)
	}
	services := detectInstalledPHPFPMServices()
	service, ok := services[normalized]
	if !ok {
		return "", fmt.Errorf("php-fpm service not found for version %s", normalized)
	}
	return service, nil
}

func fallbackSystemPHPVersion() string {
	if output, err := exec.Command("php", "-v").Output(); err == nil {
		m := regexp.MustCompile(`\b(\d+\.\d+)\b`).FindStringSubmatch(string(output))
		if len(m) >= 2 {
			return m[1]
		}
	}
	return "8.4"
}

func resolveDefaultPHPVersion(available []string) string {
	data, err := os.ReadFile(phpDefaultCfgPath)
	if err == nil {
		var cfg PHPDefaultConfig
		if json.Unmarshal(data, &cfg) == nil {
			requested := normalizePHPVersion(cfg.DefaultPHPVersion)
			if requested == "" {
				// Be tolerant of older stored formats like "PHP 8.4" or "8.4.18".
				m := regexp.MustCompile(`\b(\d+\.\d+)\b`).FindStringSubmatch(cfg.DefaultPHPVersion)
				if len(m) >= 2 {
					requested = m[1]
				}
			}
			for _, v := range available {
				if v == requested {
					return requested
				}
			}
		}
	}

	for _, v := range available {
		if v == "8.4" {
			return "8.4"
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return fallbackSystemPHPVersion()
}

func siteFPMPoolName(username, siteID string) string {
	id := strings.ReplaceAll(siteID, "-", "")
	if len(id) > 12 {
		id = id[:12]
	}
	return fmt.Sprintf("fastcp-site-%s-%s", username, id)
}

func siteSocketID(siteID string) string {
	id := strings.ReplaceAll(siteID, "-", "")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func siteFPMSocketPath(username, siteID, version string) string {
	id := siteSocketID(siteID)
	normalized := normalizePHPVersion(version)
	if normalized == "" {
		normalized = strings.TrimSpace(version)
	}
	ver := strings.ReplaceAll(normalized, ".", "")
	return filepath.Join(homeBase, username, fastcpDir, "run", fmt.Sprintf("php-%s-v%s.sock", id, ver))
}

func cleanupLegacySiteSockets(username, siteID, keepSocket string) {
	runDir := filepath.Join(homeBase, username, fastcpDir, "run")
	pattern := filepath.Join(runDir, fmt.Sprintf("php-%s*.sock", siteSocketID(siteID)))
	sockets, _ := filepath.Glob(pattern)
	for _, sock := range sockets {
		if sock == keepSocket {
			continue
		}
		// Do not remove active legacy sockets up front. Deleting a listening unix
		// socket file before the replacement pool is ready can cause intermittent 502s.
		// Only remove sockets that are clearly stale/unreachable.
		conn, err := net.DialTimeout("unix", sock, 300*time.Millisecond)
		if err != nil {
			_ = os.Remove(sock)
			continue
		}
		_ = conn.Close()
	}
	// Remove old non-versioned socket naming if present.
	_ = os.Remove(filepath.Join(runDir, fmt.Sprintf("php-%s.sock", siteSocketID(siteID))))
}

func allSocketsReady(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			return false
		}
		conn, err := net.DialTimeout("unix", p, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	return true
}

func waitForSockets(paths []string, timeout time.Duration) bool {
	if len(paths) == 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		if allSocketsReady(paths) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func unreadySockets(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			out = append(out, fmt.Sprintf("%s (missing)", p))
			continue
		}
		conn, err := net.DialTimeout("unix", p, time.Second)
		if err != nil {
			out = append(out, fmt.Sprintf("%s (not listening: %v)", p, err))
			continue
		}
		_ = conn.Close()
	}
	return out
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func detectSystemRAMMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 1024
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, convErr := strconv.Atoi(fields[1]); convErr == nil && kb > 0 {
					return kb / 1024
				}
			}
			break
		}
	}
	return 1024
}

func caddyProfileDefaults(profile string, totalRAMMB int) (string, string, string, string, string, int) {
	switch profile {
	case "low_ram":
		return "8s", "20s", "45s", "45s", "5s", 16384
	case "high_throughput":
		return "10s", "45s", "120s", "240s", "20s", 65536
	default: // balanced
		readHeaderTimeout := "10s"
		readBodyTimeout := "30s"
		writeTimeout := "90s"
		idleTimeout := "90s"
		gracePeriod := "8s"
		maxHeaderSize := 32768
		if totalRAMMB <= 2048 {
			readBodyTimeout = "20s"
			writeTimeout = "60s"
			idleTimeout = "45s"
			gracePeriod = "5s"
			maxHeaderSize = 16384
		} else if totalRAMMB >= 8192 {
			idleTimeout = "180s"
			gracePeriod = "15s"
			maxHeaderSize = 65536
		}
		return readHeaderTimeout, readBodyTimeout, writeTimeout, idleTimeout, gracePeriod, maxHeaderSize
	}
}

func defaultCaddyConfig(totalRAMMB int) *CaddyConfig {
	readHeader, readBody, writeTimeout, idleTimeout, gracePeriod, maxHeaderSize := caddyProfileDefaults("low_ram", totalRAMMB)
	return &CaddyConfig{
		Profile:       "low_ram",
		AccessLogs:    false, // errors only by default
		ExpertMode:    false,
		ReadHeader:    readHeader,
		ReadBody:      readBody,
		WriteTimeout:  writeTimeout,
		IdleTimeout:   idleTimeout,
		GracePeriod:   gracePeriod,
		MaxHeaderSize: maxHeaderSize,
	}
}

func validateDurationSetting(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return "", fmt.Errorf("%s is invalid duration", name)
	}
	if d < time.Second || d > 10*time.Minute {
		return "", fmt.Errorf("%s must be between 1s and 10m", name)
	}
	return value, nil
}

func normalizeAndValidateCaddyConfig(input *CaddyConfig, totalRAMMB int) (*CaddyConfig, error) {
	if input == nil {
		return defaultCaddyConfig(totalRAMMB), nil
	}

	cfg := *input
	cfg.Profile = strings.ToLower(strings.TrimSpace(cfg.Profile))
	if cfg.Profile == "" {
		cfg.Profile = "low_ram"
	}
	if cfg.Profile != "balanced" && cfg.Profile != "low_ram" && cfg.Profile != "high_throughput" {
		return nil, fmt.Errorf("profile must be one of: balanced, low_ram, high_throughput")
	}

	defRH, defRB, defWT, defIT, defGP, defMHS := caddyProfileDefaults(cfg.Profile, totalRAMMB)

	if !cfg.ExpertMode {
		cfg.ReadHeader = defRH
		cfg.ReadBody = defRB
		cfg.WriteTimeout = defWT
		cfg.IdleTimeout = defIT
		cfg.GracePeriod = defGP
		cfg.MaxHeaderSize = defMHS
	} else {
		var err error
		if cfg.ReadHeader == "" {
			cfg.ReadHeader = defRH
		}
		if cfg.ReadBody == "" {
			cfg.ReadBody = defRB
		}
		if cfg.WriteTimeout == "" {
			cfg.WriteTimeout = defWT
		}
		if cfg.IdleTimeout == "" {
			cfg.IdleTimeout = defIT
		}
		if cfg.GracePeriod == "" {
			cfg.GracePeriod = defGP
		}
		if cfg.MaxHeaderSize == 0 {
			cfg.MaxHeaderSize = defMHS
		}

		if cfg.ReadHeader, err = validateDurationSetting("read_header", cfg.ReadHeader); err != nil {
			return nil, err
		}
		if cfg.ReadBody, err = validateDurationSetting("read_body", cfg.ReadBody); err != nil {
			return nil, err
		}
		if cfg.WriteTimeout, err = validateDurationSetting("write_timeout", cfg.WriteTimeout); err != nil {
			return nil, err
		}
		if cfg.IdleTimeout, err = validateDurationSetting("idle_timeout", cfg.IdleTimeout); err != nil {
			return nil, err
		}
		if cfg.GracePeriod, err = validateDurationSetting("grace_period", cfg.GracePeriod); err != nil {
			return nil, err
		}
		if cfg.MaxHeaderSize < 4096 || cfg.MaxHeaderSize > 262144 {
			return nil, fmt.Errorf("max_header_size must be between 4096 and 262144 bytes")
		}
	}

	return &cfg, nil
}

func loadCaddyConfig(totalRAMMB int) *CaddyConfig {
	defaultCfg := defaultCaddyConfig(totalRAMMB)
	data, err := os.ReadFile(caddyCfgPath)
	if err != nil {
		return defaultCfg
	}
	var cfg CaddyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("invalid caddy settings file; using defaults", "error", err)
		return defaultCfg
	}
	normalized, err := normalizeAndValidateCaddyConfig(&cfg, totalRAMMB)
	if err != nil {
		slog.Warn("invalid caddy settings values; using defaults", "error", err)
		return defaultCfg
	}
	return normalized
}

func computePMAMaxChildren(totalRAMMB int) int {
	switch {
	case totalRAMMB <= 1024:
		return 2
	case totalRAMMB <= 2048:
		return 4
	case totalRAMMB <= 4096:
		return 6
	case totalRAMMB <= 8192:
		return 10
	default:
		return 14
	}
}

func computeSitePoolTuning(totalRAMMB, userLimitMB, userSiteCount int) (maxChildren int, idleTimeout string, maxRequests int) {
	if userSiteCount < 1 {
		userSiteCount = 1
	}

	// Server baseline: scales up on larger machines, conservative on small servers.
	serverBase := clampInt(totalRAMMB/768, 2, 64)

	// Respect per-user memory limits by splitting budget across that user's sites.
	if userLimitMB > 0 {
		userBudgetChildren := clampInt(userLimitMB/96, 1, 64)
		perSite := clampInt(userBudgetChildren/userSiteCount, 1, 64)
		serverBase = min(serverBase, perSite)
	}

	// Extra safeguards for tiny instances.
	switch {
	case totalRAMMB <= 1024:
		maxChildren = min(serverBase, 2)
		idleTimeout = "8s"
		maxRequests = 400
	case totalRAMMB <= 2048:
		maxChildren = min(serverBase, 3)
		idleTimeout = "10s"
		maxRequests = 500
	case totalRAMMB <= 8192:
		maxChildren = serverBase
		idleTimeout = "15s"
		maxRequests = 800
	default:
		maxChildren = serverBase
		idleTimeout = "25s"
		maxRequests = 1200
	}

	maxChildren = clampInt(maxChildren, 1, 64)
	return maxChildren, idleTimeout, maxRequests
}

func (s *Server) ensurePMAFPMPool() error {
	version := "8.4"
	poolPath := fmt.Sprintf("/etc/php/%s/fpm/pool.d/fastcp-phpmyadmin.conf", version)
	tmpDir := "/opt/fastcp/run/phpmyadmin-tmp"
	os.MkdirAll(tmpDir, 0755)
	exec.Command("chown", "-R", "www-data:www-data", tmpDir).Run()
	totalRAMMB := detectSystemRAMMB()
	pmaChildren := computePMAMaxChildren(totalRAMMB)

	content := fmt.Sprintf(`[fastcp-phpmyadmin]
user = www-data
group = www-data
listen = /opt/fastcp/run/phpmyadmin.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660

pm = ondemand
pm.max_children = %d
pm.process_idle_timeout = 10s
pm.max_requests = 700

chdir = /
clear_env = yes
security.limit_extensions = .php .phtml
request_terminate_timeout = 180s

php_admin_value[open_basedir] = /opt/fastcp/phpmyadmin:/opt/fastcp/run/phpmyadmin-tmp:/tmp:/usr/share/php
php_admin_value[upload_tmp_dir] = /opt/fastcp/run/phpmyadmin-tmp
php_admin_value[sys_temp_dir] = /opt/fastcp/run/phpmyadmin-tmp
php_admin_value[session.save_path] = /opt/fastcp/run/phpmyadmin-tmp
php_admin_flag[log_errors] = on
php_admin_value[error_log] = /var/log/fastcp/phpmyadmin-error.log
`, pmaChildren)
	if err := os.WriteFile(poolPath, []byte(content), 0644); err != nil {
		return err
	}
	if _, err := os.Stat("/var/log/fastcp/phpmyadmin-error.log"); err != nil {
		os.WriteFile("/var/log/fastcp/phpmyadmin-error.log", nil, 0644)
	}
	exec.Command("chown", "www-data:www-data", "/var/log/fastcp/phpmyadmin-error.log").Run()
	return nil
}

func (s *Server) ensureSiteFPMPools(sites map[string]*siteInfo) error {
	versionsUsed := map[string]bool{}
	desired := map[string]bool{}
	expectedSocketsByVersion := map[string][]string{}
	totalRAMMB := detectSystemRAMMB()
	userSiteCounts := map[string]int{}
	for _, site := range sites {
		if strings.EqualFold(site.Username, "root") {
			continue
		}
		userSiteCounts[site.Username]++
	}

	// Load user memory limits once so pool sizing can honor per-user caps.
	userMemoryLimits := map[string]int{}
	if db, err := sql.Open("sqlite3", "/opt/fastcp/data/fastcp.db"); err == nil {
		rows, qErr := db.Query("SELECT username, memory_mb FROM users")
		if qErr == nil {
			for rows.Next() {
				var username string
				var memoryMB int
				if scanErr := rows.Scan(&username, &memoryMB); scanErr == nil {
					userMemoryLimits[username] = memoryMB
				}
			}
			rows.Close()
		}
		db.Close()
	}

	for _, site := range sites {
		if strings.EqualFold(site.Username, "root") {
			slog.Warn("skipping root-owned site for FPM pool generation", "site_id", site.ID, "domain", site.Domain)
			continue
		}
		version := normalizePHPVersion(site.PHPVersion)
		if version == "" {
			return fmt.Errorf("invalid php version %q for site %s", site.PHPVersion, site.Domain)
		}
		versionsUsed[version] = true
		bootstrapUserEnvironment(site.Username)

		socketPath := siteFPMSocketPath(site.Username, site.ID, version)
		cleanupLegacySiteSockets(site.Username, site.ID, socketPath)
		expectedSocketsByVersion[version] = append(expectedSocketsByVersion[version], socketPath)
		socketDir := filepath.Dir(socketPath)
		os.MkdirAll(socketDir, 0755)
		exec.Command("chown", fmt.Sprintf("%s:%s", site.Username, site.Username), socketDir).Run()

		poolName := siteFPMPoolName(site.Username, site.ID)
		confPath := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", version, poolName)
		desired[confPath] = true

		userTmpDir := filepath.Join(homeBase, site.Username, ".tmp")
		openBaseDir := strings.Join([]string{
			filepath.Dir(site.DocumentRoot),
			site.DocumentRoot,
			userTmpDir,
			"/tmp",
			"/usr/share/php",
		}, ":")
		errLog := fmt.Sprintf("/var/log/fastcp/php-%s-%s-error.log", site.Username, site.Slug)
		if _, err := os.Stat(errLog); err != nil {
			os.WriteFile(errLog, nil, 0644)
		}
		exec.Command("chown", fmt.Sprintf("%s:%s", site.Username, site.Username), errLog).Run()
		userLimitMB := userMemoryLimits[site.Username]
		maxChildren, idleTimeout, maxRequests := computeSitePoolTuning(totalRAMMB, userLimitMB, userSiteCounts[site.Username])

		content := fmt.Sprintf(`[%s]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660

pm = ondemand
pm.max_children = %d
pm.process_idle_timeout = %s
pm.max_requests = %d

chdir = /
clear_env = yes
security.limit_extensions = .php .phtml
request_terminate_timeout = 300s

php_admin_value[open_basedir] = %s
php_admin_value[upload_tmp_dir] = %s
php_admin_value[sys_temp_dir] = %s
php_admin_value[session.save_path] = %s
php_admin_value[memory_limit] = %s
php_admin_value[post_max_size] = %s
php_admin_value[upload_max_filesize] = %s
php_admin_value[max_execution_time] = %d
php_admin_value[max_input_vars] = %d
php_admin_flag[log_errors] = on
php_admin_value[error_log] = %s
`, poolName, site.Username, site.Username, socketPath, site.Username, site.Username, maxChildren, idleTimeout, maxRequests,
			openBaseDir,
			filepath.Join(userTmpDir, "uploads"),
			userTmpDir,
			filepath.Join(userTmpDir, "sessions"),
			site.PHPMemoryLimit,
			site.PHPPostMaxSize,
			site.PHPUploadMaxSize,
			site.PHPMaxExecutionTime,
			site.PHPMaxInputVars,
			errLog,
		)
		if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write site FPM pool for %s: %w", site.Domain, err)
		}
	}

	// Cleanup stale FastCP site pool files
	for _, version := range []string{"8.2", "8.3", "8.4", "8.5"} {
		pattern := fmt.Sprintf("/etc/php/%s/fpm/pool.d/fastcp-site-*.conf", version)
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if !desired[f] {
				os.Remove(f)
			}
		}
	}

	// Shared phpMyAdmin pool on 8.4
	if err := s.ensurePMAFPMPool(); err == nil {
		versionsUsed["8.4"] = true
		expectedSocketsByVersion["8.4"] = append(expectedSocketsByVersion["8.4"], "/opt/fastcp/run/phpmyadmin.sock")
	}

	for version := range versionsUsed {
		restartDirect := func() {
			// Non-systemd environments (e.g. local Docker dev): run php-fpm directly.
			candidates := phpFPMBinaryCandidates(version)
			bin := ""
			for _, c := range candidates {
				if strings.HasPrefix(c, "/") {
					if _, err := os.Stat(c); err == nil {
						bin = c
						break
					}
					continue
				}
				if p, err := exec.LookPath(c); err == nil {
					bin = p
					break
				}
			}
			if bin == "" {
				slog.Warn("php-fpm binary not found for version", "version", version)
				return
			}
			if err := exec.Command(bin, "-t").Run(); err != nil {
				slog.Warn("php-fpm config test failed", "version", version, "binary", bin, "error", err)
				return
			}
			pidFile := fmt.Sprintf("/run/php/php%s-fpm.pid", version)
			if data, err := os.ReadFile(pidFile); err == nil {
				pid := strings.TrimSpace(string(data))
				if pid != "" {
					// Only reload if PID belongs to a live FPM master (not a zombie/stale PID).
					if out, err := exec.Command("ps", "-p", pid, "-o", "stat=", "-o", "args=").Output(); err == nil {
						line := strings.TrimSpace(string(out))
						if line != "" && !strings.HasPrefix(line, "Z") && strings.Contains(line, "php-fpm: master process") {
							if err := exec.Command("kill", "-USR2", pid).Run(); err == nil {
								return
							}
						}
					}
				}
				// Stale PID file: remove and perform cold start below.
				_ = os.Remove(pidFile)
			}
			if err := exec.Command(bin, "-D").Run(); err != nil {
				slog.Warn("failed to reload/start php-fpm directly", "version", version, "binary", bin, "error", err)
			}
		}

		if s.hasSystemd() {
			service, svcErr := resolvePHPFPMServiceName(version)
			if svcErr != nil {
				slog.Warn("php-fpm service resolution failed", "version", version, "error", svcErr)
				restartDirect()
				if !waitForSockets(expectedSocketsByVersion[version], 35*time.Second) {
					return fmt.Errorf("php %s FPM sockets not available after restart: %s", version, strings.Join(unreadySockets(expectedSocketsByVersion[version]), "; "))
				}
				continue
			}
			_ = s.runSystemctl("enable", service)
			if err := s.serviceReloadOrRestart(service); err != nil {
				slog.Warn("reload-or-restart failed for php-fpm service", "service", service, "error", err)
			}
			// Verify expected sockets for this version exist; if not, force restart once.
			if !waitForSockets(expectedSocketsByVersion[version], 12*time.Second) {
				_ = s.serviceReloadOrRestart(service)
			}
			// Fallback when systemctl exists but service management is unavailable.
			if !waitForSockets(expectedSocketsByVersion[version], 12*time.Second) {
				// Fallback when systemctl exists but service management is unavailable.
				restartDirect()
			}
			if !waitForSockets(expectedSocketsByVersion[version], 35*time.Second) {
				return fmt.Errorf("php %s FPM sockets not available after reload: %s", version, strings.Join(unreadySockets(expectedSocketsByVersion[version]), "; "))
			}
			continue
		}
		restartDirect()
		// Cold-start path should produce all sockets for this version.
		if !waitForSockets(expectedSocketsByVersion[version], 35*time.Second) {
			return fmt.Errorf("php %s FPM sockets not available after restart: %s", version, strings.Join(unreadySockets(expectedSocketsByVersion[version]), "; "))
		}
	}
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

	// Fetch all sites (with fallback for older schemas during rolling updates)
	rows, err := db.Query(`SELECT id, domain, username, document_root, COALESCE(slug, ''), COALESCE(php_version, '8.4'),
		COALESCE(force_https, 1),
		COALESCE(compression_enabled, 1), COALESCE(gzip_enabled, 1), COALESCE(zstd_enabled, 1),
		COALESCE(cache_control_enabled, 0), COALESCE(cache_control_value, ''),
		COALESCE(php_memory_limit, '256M'), COALESCE(php_post_max_size, '64M'), COALESCE(php_upload_max_filesize, '64M'),
		COALESCE(php_max_execution_time, 300), COALESCE(php_max_input_vars, 5000)
		FROM sites`)
	legacySchema := false
	if err != nil {
		rows, err = db.Query("SELECT id, domain, username, document_root, COALESCE(slug, '') FROM sites")
		if err != nil {
			// If no sites table yet, use default config
			slog.Warn("no sites table found, using default config", "error", err)
			return nil
		}
		legacySchema = true
	}
	defer rows.Close()

	// Build sites map
	sitesMap := make(map[string]*siteInfo)
	for rows.Next() {
		var site siteInfo
		if legacySchema {
			if err := rows.Scan(&site.ID, &site.Domain, &site.Username, &site.DocumentRoot, &site.Slug); err != nil {
				continue
			}
			site.ForceHTTPS = true
			site.CompressionEnabled = true
			site.GzipEnabled = true
			site.ZstdEnabled = true
			site.CacheControlEnabled = false
			site.CacheControlValue = ""
			site.PHPVersion = "8.4"
			site.PHPMemoryLimit = "256M"
			site.PHPPostMaxSize = "64M"
			site.PHPUploadMaxSize = "64M"
			site.PHPMaxExecutionTime = 300
			site.PHPMaxInputVars = 5000
		} else {
			if err := rows.Scan(
				&site.ID, &site.Domain, &site.Username, &site.DocumentRoot, &site.Slug, &site.PHPVersion,
				&site.ForceHTTPS,
				&site.CompressionEnabled, &site.GzipEnabled, &site.ZstdEnabled,
				&site.CacheControlEnabled, &site.CacheControlValue,
				&site.PHPMemoryLimit, &site.PHPPostMaxSize, &site.PHPUploadMaxSize,
				&site.PHPMaxExecutionTime, &site.PHPMaxInputVars,
			); err != nil {
				continue
			}
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

	// Generate PHP-FPM pool files for all sites.
	// Must fail hard: continuing with a Caddy reload against missing/invalid FPM sockets leads to 502 responses.
	if err := s.ensureSiteFPMPools(sitesMap); err != nil {
		return fmt.Errorf("failed to ensure site FPM pools: %w", err)
	}

	// Generate main Caddyfile (direct site serving with php_fastcgi)
	var mainBuf strings.Builder
	devMode := os.Getenv("FASTCP_DEV") == "1"
	totalRAMMB := detectSystemRAMMB()
	caddyCfg := loadCaddyConfig(totalRAMMB)
	devGlobal := ""
	if devMode {
		// Local Docker/dev mode: keep HTTP available and disable automatic HTTPS redirects.
		devGlobal = "    auto_https off\n"
	}
	mainBuf.WriteString(`# FastCP Main Caddyfile
# DO NOT EDIT - This file is auto-generated by FastCP

{
    admin localhost:2019
    grace_period ` + caddyCfg.GracePeriod + `
    # Default to error-only logging in production to minimize I/O overhead.
    log {
        level ERROR
    }
    servers {
        timeouts {
            read_header ` + caddyCfg.ReadHeader + `
            read_body ` + caddyCfg.ReadBody + `
            write ` + caddyCfg.WriteTimeout + `
            idle ` + caddyCfg.IdleTimeout + `
        }
        max_header_size ` + strconv.Itoa(caddyCfg.MaxHeaderSize) + `
    }
` + devGlobal + `}

`)

	domainAddress := func(domain string, forceHTTPS bool) string {
		if devMode {
			return "http://" + domain
		}
		if forceHTTPS {
			return domain
		}
		return "http://" + domain + ", https://" + domain
	}
	redirectTargetScheme := func(forceHTTPS bool) string {
		if devMode {
			return "http"
		}
		if forceHTTPS {
			return "https"
		}
		return "{scheme}"
	}

	// phpMyAdmin internal backend (Go auth proxy forwards to this).
	// Force HTTP explicitly so reverse proxy scheme always matches.
	mainBuf.WriteString(`http://127.0.0.1:2088 {
    root * /opt/fastcp/phpmyadmin
    php_fastcgi unix//opt/fastcp/run/phpmyadmin.sock
    file_server
}

`)

	for _, site := range sitesMap {
		logsDir := filepath.Join(homeBase, site.Username, appsDir, site.SafeDomain, "logs")
		if caddyCfg.AccessLogs {
			os.MkdirAll(logsDir, 0755)
		}
		poolSocket := siteFPMSocketPath(site.Username, site.ID, site.PHPVersion)

		for _, domain := range site.Domains {
			if site.IsSuspended {
				mainBuf.WriteString(fmt.Sprintf(`# Site: %s (User: %s) [SUSPENDED]
%s {
    root * /var/www/suspended
    file_server
    try_files {path} /index.html
}

`, domain.Domain, site.Username, domainAddress(domain.Domain, site.ForceHTTPS)))
			} else if domain.RedirectToPrimary && site.PrimaryDomain != domain.Domain {
				mainBuf.WriteString(fmt.Sprintf(`# Redirect: %s -> %s (User: %s)
%s {
    redir %s://%s{uri} permanent
}

`, domain.Domain, site.PrimaryDomain, site.Username, domainAddress(domain.Domain, site.ForceHTTPS), redirectTargetScheme(site.ForceHTTPS), site.PrimaryDomain))
			} else {
				compressionLine := ""
				if site.CompressionEnabled {
					var algos []string
					if site.ZstdEnabled {
						algos = append(algos, "zstd")
					}
					if site.GzipEnabled {
						algos = append(algos, "gzip")
					}
					if len(algos) > 0 {
						compressionLine = fmt.Sprintf("    encode %s\n", strings.Join(algos, " "))
					}
				}

				cacheControlLine := ""
				if site.CacheControlEnabled {
					cacheVal := strings.TrimSpace(site.CacheControlValue)
					cacheVal = strings.ReplaceAll(cacheVal, "\r", "")
					cacheVal = strings.ReplaceAll(cacheVal, "\n", "")
					if cacheVal != "" {
						cacheControlLine = fmt.Sprintf("    header Cache-Control %q\n", cacheVal)
					}
				}

				accessLogLine := ""
				if caddyCfg.AccessLogs {
					accessLogLine = fmt.Sprintf(`
    log {
        output file %s/access.log
    }`, logsDir)
				}

				mainBuf.WriteString(fmt.Sprintf(`# Site: %s (User: %s)%s
%s {
    root * %s
%s%s    php_fastcgi unix/%s
    file_server
%s
}

`, domain.Domain, site.Username, func() string {
					if domain.IsPrimary {
						return " [PRIMARY]"
					}
					return ""
				}(), domainAddress(domain.Domain, site.ForceHTTPS), site.DocumentRoot, compressionLine, cacheControlLine, poolSocket, accessLogLine))
			}
		}
	}

	mainBuf.WriteString(`# Default fallback for unconfigured domains
:80, :443 {
    respond "FastCP - No site configured for this domain" 404
}
`)

	// Write main Caddyfile
	if err := os.WriteFile(caddyConfig, []byte(mainBuf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write main Caddyfile: %w", err)
	}

	slog.Info("generated Caddyfile", "sites", len(sitesMap))
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
	buf.WriteString(fmt.Sprintf(`# FastCP per-user Caddy config: %s
# DO NOT EDIT - This file is auto-generated by FastCP

{
    admin off
}

:80 {
    bind unix/%s

`, username, socketPath))

	// phpMyAdmin handler -- only reachable when Go proxy sets Host: phpmyadmin.internal
	buf.WriteString(`    @phpmyadmin host phpmyadmin.internal
    handle @phpmyadmin {
        root * /opt/fastcp/phpmyadmin
        php_server
    }

`)

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
		compressionLine := ""
		if site.CompressionEnabled {
			var algos []string
			if site.ZstdEnabled {
				algos = append(algos, "zstd")
			}
			if site.GzipEnabled {
				algos = append(algos, "gzip")
			}
			if len(algos) > 0 {
				compressionLine = fmt.Sprintf("        encode %s\n", strings.Join(algos, " "))
			}
		}

		cacheControlLine := ""
		if site.CacheControlEnabled {
			cacheVal := strings.TrimSpace(site.CacheControlValue)
			cacheVal = strings.ReplaceAll(cacheVal, "\r", "")
			cacheVal = strings.ReplaceAll(cacheVal, "\n", "")
			if cacheVal != "" {
				cacheControlLine = fmt.Sprintf("        header Cache-Control %q\n", cacheVal)
			}
		}

		buf.WriteString(fmt.Sprintf(`    @%s host %s
    handle @%s {
        root * %s
%s%s
        php_server
    }

`, matcherName, hostList, matcherName, site.DocumentRoot, compressionLine, cacheControlLine))
	}

	// Per-user temp directory paths (directories are created by bootstrapUserEnvironment)
	userTmpDir := filepath.Join(homeBase, username, ".tmp")
	userSessionDir := filepath.Join(userTmpDir, "sessions")
	userUploadDir := filepath.Join(userTmpDir, "uploads")
	userCacheDir := filepath.Join(userTmpDir, "cache")
	userWsdlDir := filepath.Join(userTmpDir, "wsdl")

	var docRoots []string
	for _, site := range sites {
		docRoots = append(docRoots, filepath.Dir(site.DocumentRoot))
		docRoots = append(docRoots, site.DocumentRoot)
	}
	openBasedir := strings.Join(docRoots, ":") + ":" + userTmpDir + ":/opt/fastcp/phpmyadmin"

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

	return nil
}

type siteInfo struct {
	ID                  string
	Domain              string
	Username            string
	Slug                string
	PHPVersion          string
	ForceHTTPS          bool
	DocumentRoot        string
	SafeDomain          string
	Domains             []siteDomainInfo
	PrimaryDomain       string
	IsSuspended         bool
	CompressionEnabled  bool
	GzipEnabled         bool
	ZstdEnabled         bool
	CacheControlEnabled bool
	CacheControlValue   string
	PHPMemoryLimit      string
	PHPPostMaxSize      string
	PHPUploadMaxSize    string
	PHPMaxExecutionTime int
	PHPMaxInputVars     int
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

func (s *Server) handleResetDatabasePassword(ctx context.Context, params json.RawMessage) (any, error) {
	var req ResetDatabasePasswordRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	slog.Info("resetting database password", "db_name", req.DBName, "db_user", req.DBUser)

	db, err := sql.Open("mysql", fmt.Sprintf("root@unix(%s)/", mysqlSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer db.Close()

	// Rotate password for both localhost (socket) and 127.0.0.1 (TCP).
	for _, host := range []string{"localhost", "127.0.0.1"} {
		_, err = db.Exec(fmt.Sprintf("ALTER USER IF EXISTS '%s'@'%s' IDENTIFIED BY '%s'", req.DBUser, host, req.Password))
		if err != nil {
			return nil, fmt.Errorf("failed to reset password for user host %s: %w", host, err)
		}
	}

	_, err = db.Exec("FLUSH PRIVILEGES")
	if err != nil {
		return nil, fmt.Errorf("failed to flush privileges: %w", err)
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

	// Get memory info.
	// Use "used" memory semantics closer to htop/free:
	// used = MemTotal - MemFree - Buffers - Cached - SReclaimable + Shmem
	// Fallback to MemTotal - MemAvailable when detailed fields are missing.
	var memTotal, memAvail, memFree, buffers, cached, sReclaimable, shmem uint64
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(line, "MemTotal: %d kB", &memTotal)
				memTotal *= 1024
			} else if strings.HasPrefix(line, "MemFree:") {
				fmt.Sscanf(line, "MemFree: %d kB", &memFree)
				memFree *= 1024
			} else if strings.HasPrefix(line, "Buffers:") {
				fmt.Sscanf(line, "Buffers: %d kB", &buffers)
				buffers *= 1024
			} else if strings.HasPrefix(line, "Cached:") {
				fmt.Sscanf(line, "Cached: %d kB", &cached)
				cached *= 1024
			} else if strings.HasPrefix(line, "SReclaimable:") {
				fmt.Sscanf(line, "SReclaimable: %d kB", &sReclaimable)
				sReclaimable *= 1024
			} else if strings.HasPrefix(line, "Shmem:") {
				fmt.Sscanf(line, "Shmem: %d kB", &shmem)
				shmem *= 1024
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(line, "MemAvailable: %d kB", &memAvail)
				memAvail *= 1024
			}
		}
	}
	memUsed := uint64(0)
	if memTotal > 0 {
		detailedKnown := memFree > 0 || buffers > 0 || cached > 0 || sReclaimable > 0 || shmem > 0
		if detailedKnown {
			cacheLike := buffers + cached + sReclaimable
			if memTotal > memFree+cacheLike {
				memUsed = memTotal - memFree - cacheLike + shmem
			}
		} else if memAvail > 0 && memTotal >= memAvail {
			memUsed = memTotal - memAvail
		}
		if memUsed > memTotal {
			memUsed = memTotal
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

	// Detect installed PHP versions and resolve configured default for new sites.
	availablePHPVersions := detectAvailablePHPVersions()
	phpVersion := resolveDefaultPHPVersion(availablePHPVersions)

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

	// Get Caddy version
	caddyVersion := ""
	if output, err := exec.Command("caddy", "version").Output(); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(output)))
		if len(fields) > 0 {
			caddyVersion = fields[0]
		}
	}

	// Kernel / architecture
	kernelVersion := ""
	if output, err := exec.Command("uname", "-r").Output(); err == nil {
		kernelVersion = strings.TrimSpace(string(output))
	}

	architecture := ""
	if output, err := exec.Command("uname", "-m").Output(); err == nil {
		architecture = strings.TrimSpace(string(output))
	}

	// OS name from /etc/os-release if available.
	osName := "Linux"
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				osName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	// Aggregate counts for dashboard widgets.
	totalUsers, totalWebsites, totalDatabases := 0, 0, 0
	if db, err := sql.Open("sqlite3", "/opt/fastcp/data/fastcp.db"); err == nil {
		defer db.Close()
		_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&totalUsers)
		_ = db.QueryRow("SELECT COUNT(*) FROM sites").Scan(&totalWebsites)
		_ = db.QueryRow("SELECT COUNT(*) FROM databases").Scan(&totalDatabases)
	}

	return &SystemStatus{
		Hostname:             hostname,
		OS:                   osName,
		Uptime:               uptime,
		LoadAverage:          loadAvg,
		MemoryTotal:          memTotal,
		MemoryUsed:           memUsed,
		DiskTotal:            diskTotal,
		DiskUsed:             diskUsed,
		PHPVersion:           phpVersion,
		MySQLVersion:         mysqlVersion,
		CaddyVersion:         caddyVersion,
		PHPAvailableVersions: availablePHPVersions,
		KernelVersion:        kernelVersion,
		Architecture:         architecture,
		TotalUsers:           totalUsers,
		TotalWebsites:        totalWebsites,
		TotalDatabases:       totalDatabases,
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

const (
	mysqlCnfPath      = "/etc/mysql/conf.d/fastcp.cnf"
	sshdMainConfig    = "/etc/ssh/sshd_config"
	sshdFastcpConf    = "/etc/ssh/sshd_config.d/fastcp.conf"
	phpDefaultCfgPath = "/opt/fastcp/config/php-defaults.json"
	caddyCfgPath      = "/opt/fastcp/config/caddy-settings.json"
	controlPanelPort  = 2050
)

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func normalizeFirewallProtocol(protocol string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	if p != "udp" {
		return "tcp"
	}
	return p
}

func normalizeFirewallIPVersion(ipVersion string) string {
	v := strings.ToLower(strings.TrimSpace(ipVersion))
	switch v {
	case "ipv4", "ipv6":
		return v
	default:
		return "both"
	}
}

func parseRuleIPVersion(rule string) string {
	if strings.Contains(strings.ToLower(rule), "(v6)") {
		return "ipv6"
	}
	return "ipv4"
}

func parseUFWNumberedLine(line string) (int, string, string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return 0, "", "", "", false
	}
	endIdx := strings.Index(trimmed, "]")
	if endIdx < 0 {
		return 0, "", "", "", false
	}
	numStr := strings.TrimSpace(trimmed[1:endIdx])
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, "", "", "", false
	}
	rest := strings.TrimSpace(trimmed[endIdx+1:])
	parts := strings.Fields(rest)
	if len(parts) < 3 {
		return 0, "", "", "", false
	}
	actionIdx := -1
	for i, p := range parts {
		up := strings.ToUpper(p)
		if up == "ALLOW" || up == "DENY" {
			actionIdx = i
			break
		}
	}
	if actionIdx <= 0 {
		return 0, "", "", "", false
	}
	rule := strings.Join(parts[:actionIdx], " ")
	action := strings.ToUpper(parts[actionIdx])
	from := strings.Join(parts[actionIdx+1:], " ")
	return num, rule, action, from, true
}

func listMatchingUFWRuleNumbers(port int, protocol, action, ipVersion string) ([]int, error) {
	specPrefix := fmt.Sprintf("%d/%s", port, protocol)
	output, err := exec.Command("ufw", "status", "numbered").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect ufw numbered rules: %w: %s", err, strings.TrimSpace(string(output)))
	}
	matches := []int{}
	for _, line := range strings.Split(string(output), "\n") {
		num, rule, ruleAction, _, ok := parseUFWNumberedLine(line)
		if !ok {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(rule), strings.ToLower(specPrefix)) {
			continue
		}
		if action != "" && strings.ToUpper(action) != ruleAction {
			continue
		}
		ruleIPVersion := parseRuleIPVersion(rule)
		if ipVersion != "both" && ipVersion != ruleIPVersion {
			continue
		}
		matches = append(matches, num)
	}
	return matches, nil
}

func deleteUFWRulesByNumbers(numbers []int) error {
	if len(numbers) == 0 {
		return nil
	}
	// Delete highest numbers first because ufw reindexes after each delete.
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] > numbers[j] })
	var failed []string
	for _, num := range numbers {
		if output, err := exec.Command("ufw", "--force", "delete", strconv.Itoa(num)).CombinedOutput(); err != nil {
			failed = append(failed, fmt.Sprintf("#%d: %s", num, strings.TrimSpace(string(output))))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to delete ufw rules: %s", strings.Join(failed, "; "))
	}
	return nil
}

func reloadUFWIfActive() error {
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to read ufw status before reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(string(out))), "status: active") {
		return nil
	}
	if output, err := exec.Command("ufw", "reload").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload ufw: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Server) parseUFWStatus() (*FirewallStatus, error) {
	status := &FirewallStatus{
		Installed:        binaryExists("ufw"),
		Enabled:          false,
		ControlPanelPort: controlPanelPort,
		Rules:            []FirewallRule{},
	}
	if !status.Installed {
		return status, nil
	}

	output, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		msg := strings.ToLower(strings.TrimSpace(string(output)))
		// In restricted/containerized environments ufw may be installed but iptables access is blocked.
		// Return a safe status object so UI stays usable instead of hard-failing the entire settings page.
		if strings.Contains(msg, "permission denied") || strings.Contains(msg, "problem running iptables") {
			slog.Warn("ufw status permission issue", "error", err, "output", strings.TrimSpace(string(output)))
			return status, nil
		}
		return nil, fmt.Errorf("failed to read ufw status: %w: %s", err, strings.TrimSpace(string(output)))
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return status, nil
	}
	firstLine := strings.TrimSpace(strings.ToLower(lines[0]))
	status.Enabled = strings.HasPrefix(firstLine, "status: active")

	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "To") || strings.HasPrefix(trimmed, "--") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			continue
		}

		actionIdx := -1
		for i, p := range parts {
			upper := strings.ToUpper(p)
			if upper == "ALLOW" || upper == "DENY" {
				actionIdx = i
				break
			}
		}
		if actionIdx == -1 {
			continue
		}
		rule := strings.Join(parts[:actionIdx], " ")
		action := strings.ToUpper(parts[actionIdx])
		from := strings.Join(parts[actionIdx+1:], " ")
		status.Rules = append(status.Rules, FirewallRule{
			Rule:      rule,
			Action:    action,
			From:      from,
			IPVersion: parseRuleIPVersion(rule),
		})
	}

	return status, nil
}

func (s *Server) ensureControlPanelFirewallRule() error {
	spec := fmt.Sprintf("%d/tcp", controlPanelPort)
	if output, err := exec.Command("ufw", "allow", spec).CombinedOutput(); err != nil {
		combined := strings.TrimSpace(string(output))
		if !strings.Contains(strings.ToLower(combined), "skipping") && !strings.Contains(strings.ToLower(combined), "exists") {
			return fmt.Errorf("failed to allow control panel port %d: %w: %s", controlPanelPort, err, combined)
		}
	}
	return nil
}

func allowUFWPort(port int) error {
	spec := fmt.Sprintf("%d/tcp", port)
	if output, err := exec.Command("ufw", "allow", spec).CombinedOutput(); err != nil {
		combined := strings.TrimSpace(string(output))
		lower := strings.ToLower(combined)
		if !strings.Contains(lower, "skipping") && !strings.Contains(lower, "exists") {
			return fmt.Errorf("failed to allow port %d: %w: %s", port, err, combined)
		}
	}
	return nil
}

func ensureBaselineFirewallRules() error {
	// Default-deny inbound traffic, then explicitly allow baseline service ports.
	if output, err := exec.Command("ufw", "default", "deny", "incoming").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set ufw default deny incoming: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("ufw", "default", "allow", "outgoing").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set ufw default allow outgoing: %w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, port := range []int{80, 443, controlPanelPort} {
		if err := allowUFWPort(port); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleGetFirewallStatus(ctx context.Context, params json.RawMessage) (any, error) {
	return s.parseUFWStatus()
}

func (s *Server) handleInstallFirewall(ctx context.Context, params json.RawMessage) (any, error) {
	if binaryExists("ufw") {
		return map[string]string{"status": "ok", "message": "UFW already installed"}, nil
	}
	cmd := exec.Command("bash", "-lc", "DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ufw")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to install ufw: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := ensureBaselineFirewallRules(); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok", "message": "UFW installed"}, nil
}

func detectRcloneStatus() *RcloneStatus {
	path, err := exec.LookPath("rclone")
	if err != nil {
		return &RcloneStatus{Installed: false}
	}
	status := &RcloneStatus{Installed: true, Path: path}
	if output, cmdErr := exec.Command(path, "version").CombinedOutput(); cmdErr == nil {
		firstLine := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
		if firstLine != "" {
			status.Version = firstLine
		}
	}
	return status
}

func (s *Server) handleGetRcloneStatus(ctx context.Context, params json.RawMessage) (any, error) {
	return detectRcloneStatus(), nil
}

func (s *Server) handleInstallRclone(ctx context.Context, params json.RawMessage) (any, error) {
	current := detectRcloneStatus()
	if current.Installed {
		return current, nil
	}
	if output, err := runAptCommand("update", "-qq"); err != nil {
		return nil, fmt.Errorf("failed to update apt indexes: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := runAptCommand("install", "-y", "-qq", "rclone"); err != nil {
		return nil, fmt.Errorf("failed to install rclone: %w: %s", err, strings.TrimSpace(string(output)))
	}
	installed := detectRcloneStatus()
	if !installed.Installed {
		return nil, fmt.Errorf("rclone installation finished but binary is still not available")
	}
	return installed, nil
}

func (s *Server) handleSetFirewallEnabled(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !binaryExists("ufw") {
		return nil, fmt.Errorf("ufw is not installed")
	}

	if req.Enabled {
		if err := ensureBaselineFirewallRules(); err != nil {
			return nil, err
		}
		if output, err := exec.Command("ufw", "--force", "enable").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to enable ufw: %w: %s", err, strings.TrimSpace(string(output)))
		}
	} else {
		if output, err := exec.Command("ufw", "disable").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to disable ufw: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleFirewallAllowPort(ctx context.Context, params json.RawMessage) (any, error) {
	var req FirewallRuleRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !binaryExists("ufw") {
		return nil, fmt.Errorf("ufw is not installed")
	}
	if req.Port < 1 || req.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	protocol := normalizeFirewallProtocol(req.Protocol)
	ipVersion := normalizeFirewallIPVersion(req.IPVersion)
	spec := fmt.Sprintf("%d/%s", req.Port, protocol)
	if output, err := exec.Command("ufw", "allow", spec).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to allow port: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if ipVersion == "ipv4" || ipVersion == "ipv6" {
		other := "ipv4"
		if ipVersion == "ipv4" {
			other = "ipv6"
		}
		if nums, err := listMatchingUFWRuleNumbers(req.Port, protocol, "ALLOW", other); err == nil {
			if err := deleteUFWRulesByNumbers(nums); err != nil {
				return nil, err
			}
		}
	}
	if err := reloadUFWIfActive(); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleFirewallDenyPort(ctx context.Context, params json.RawMessage) (any, error) {
	var req FirewallRuleRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !binaryExists("ufw") {
		return nil, fmt.Errorf("ufw is not installed")
	}
	if req.Port < 1 || req.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	if req.Port == controlPanelPort {
		return nil, fmt.Errorf("cannot block control panel port %d", controlPanelPort)
	}
	protocol := normalizeFirewallProtocol(req.Protocol)
	ipVersion := normalizeFirewallIPVersion(req.IPVersion)
	spec := fmt.Sprintf("%d/%s", req.Port, protocol)
	if output, err := exec.Command("ufw", "deny", spec).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to deny port: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if ipVersion == "ipv4" || ipVersion == "ipv6" {
		other := "ipv4"
		if ipVersion == "ipv4" {
			other = "ipv6"
		}
		if nums, err := listMatchingUFWRuleNumbers(req.Port, protocol, "DENY", other); err == nil {
			if err := deleteUFWRulesByNumbers(nums); err != nil {
				return nil, err
			}
		}
	}
	if err := reloadUFWIfActive(); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleFirewallDeleteRule(ctx context.Context, params json.RawMessage) (any, error) {
	var req FirewallRuleRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !binaryExists("ufw") {
		return nil, fmt.Errorf("ufw is not installed")
	}
	if req.Port < 1 || req.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	if req.Port == controlPanelPort {
		return nil, fmt.Errorf("cannot remove rule for control panel port %d", controlPanelPort)
	}
	protocol := normalizeFirewallProtocol(req.Protocol)
	ipVersion := normalizeFirewallIPVersion(req.IPVersion)
	allowNums, err := listMatchingUFWRuleNumbers(req.Port, protocol, "ALLOW", ipVersion)
	if err != nil {
		return nil, err
	}
	denyNums, err := listMatchingUFWRuleNumbers(req.Port, protocol, "DENY", ipVersion)
	if err != nil {
		return nil, err
	}
	if err := deleteUFWRulesByNumbers(append(allowNums, denyNums...)); err != nil {
		return nil, err
	}
	remainingAllow, _ := listMatchingUFWRuleNumbers(req.Port, protocol, "ALLOW", ipVersion)
	remainingDeny, _ := listMatchingUFWRuleNumbers(req.Port, protocol, "DENY", ipVersion)
	if len(remainingAllow)+len(remainingDeny) > 0 {
		return nil, fmt.Errorf("firewall rule still exists after delete; please retry")
	}
	if err := reloadUFWIfActive(); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleGetPHPDefaultConfig(ctx context.Context, params json.RawMessage) (any, error) {
	available := detectAvailablePHPVersions()
	return &PHPDefaultConfig{
		DefaultPHPVersion:    resolveDefaultPHPVersion(available),
		AvailablePHPVersions: available,
	}, nil
}

func (s *Server) handleSetPHPDefaultConfig(ctx context.Context, params json.RawMessage) (any, error) {
	var cfg PHPDefaultConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	requested := normalizePHPVersion(cfg.DefaultPHPVersion)
	if requested == "" {
		return nil, fmt.Errorf("invalid php version format: %q", cfg.DefaultPHPVersion)
	}
	available := detectAvailablePHPVersions()
	supported := false
	for _, v := range available {
		if v == requested {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("php version %s is not installed on this server", requested)
	}

	if err := os.MkdirAll(filepath.Dir(phpDefaultCfgPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create php defaults directory: %w", err)
	}
	writeCfg := &PHPDefaultConfig{DefaultPHPVersion: requested}
	data, err := json.MarshalIndent(writeCfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal php default config: %w", err)
	}
	if err := os.WriteFile(phpDefaultCfgPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write php default config: %w", err)
	}

	slog.Info("updated default php version", "version", requested)
	return map[string]string{"status": "ok"}, nil
}

func runAptCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("apt-get", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_SUSPEND=1")
	return cmd.CombinedOutput()
}

func phpPackageExists(pkg string) bool {
	return exec.Command("apt-cache", "show", pkg).Run() == nil
}

func resolveInstallablePHPPackages(version string) ([]string, error) {
	base := []string{
		"php" + version,
		"php" + version + "-fpm",
	}
	modules := []string{
		"bcmath", "bz2", "cli", "common", "curl", "gd", "gmp", "igbinary", "imagick",
		"imap", "intl", "mbstring", "mysql", "opcache", "readline", "redis", "soap",
		"sqlite3", "xml", "xmlrpc", "zip",
	}
	pkgs := make([]string, 0, len(base)+len(modules))
	for _, pkg := range base {
		if phpPackageExists(pkg) {
			pkgs = append(pkgs, pkg)
		}
	}
	hasFPM := false
	for _, p := range pkgs {
		if p == "php"+version+"-fpm" {
			hasFPM = true
			break
		}
	}
	if !hasFPM {
		return nil, fmt.Errorf("php%s-fpm is not available in apt repositories", version)
	}
	for _, m := range modules {
		pkg := "php" + version + "-" + m
		if phpPackageExists(pkg) {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs, nil
}

func (s *Server) startOrRestartPHPFPM(version string) error {
	service, svcErr := resolvePHPFPMServiceName(version)
	if s.hasSystemd() && svcErr == nil {
		_ = s.runSystemctl("enable", service)
		return s.serviceReloadOrRestart(service)
	}
	if s.hasSystemd() && svcErr != nil {
		return svcErr
	}

	bin := ""
	for _, candidate := range phpFPMBinaryCandidates(version) {
		if strings.HasPrefix(candidate, "/") {
			if _, err := os.Stat(candidate); err == nil {
				bin = candidate
				break
			}
			continue
		}
		if p, err := exec.LookPath(candidate); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		return fmt.Errorf("php-fpm binary not found for version %s", version)
	}
	if output, err := exec.Command(bin, "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("php-fpm config test failed for %s: %w: %s", version, err, strings.TrimSpace(string(output)))
	}
	pidFile := fmt.Sprintf("/run/php/php%s-fpm.pid", version)
	if data, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(data))
		if pid != "" {
			if killErr := exec.Command("kill", "-USR2", pid).Run(); killErr == nil {
				return nil
			}
		}
		_ = os.Remove(pidFile)
	}
	if output, err := exec.Command(bin, "-D").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start php-fpm %s: %w: %s", version, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Server) handleInstallPHPVersion(ctx context.Context, params json.RawMessage) (any, error) {
	var req PHPVersionInstallRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	version := normalizePHPVersion(req.Version)
	if version == "" {
		return nil, fmt.Errorf("invalid php version format: %q", req.Version)
	}
	if version != strings.TrimSpace(req.Version) {
		return nil, fmt.Errorf("unsupported php version %q", req.Version)
	}

	for _, v := range detectAvailablePHPVersions() {
		if v == version {
			return map[string]string{"status": "ok", "message": "php version already installed"}, nil
		}
	}

	if output, err := runAptCommand("update", "-qq"); err != nil {
		return nil, fmt.Errorf("failed to update apt indexes: %w: %s", err, strings.TrimSpace(string(output)))
	}
	pkgs, err := resolveInstallablePHPPackages(version)
	if err != nil {
		return nil, err
	}
	args := append([]string{"install", "-y", "-qq"}, pkgs...)
	if output, err := runAptCommand(args...); err != nil {
		return nil, fmt.Errorf("failed to install php%s packages: %w: %s", version, err, strings.TrimSpace(string(output)))
	}
	if err := s.startOrRestartPHPFPM(version); err != nil {
		return nil, err
	}
	if err := s.generateCaddyfile(); err != nil {
		return nil, fmt.Errorf("failed to regenerate Caddyfile after php install: %w", err)
	}
	if !s.isCaddyRunning() {
		if err := s.startCaddy(); err != nil {
			return nil, fmt.Errorf("failed to start Caddy after php install: %w", err)
		}
	} else if err := s.reloadCaddy(); err != nil {
		return nil, fmt.Errorf("failed to reload Caddy after php install: %w", err)
	}
	slog.Info("installed php runtime on demand", "version", version)
	return map[string]string{"status": "ok", "message": "php version installed"}, nil
}

func (s *Server) handleGetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	cfg := &MySQLConfig{BufferPoolMB: 128, MaxConnections: 30, PerfSchema: false}

	// Detect RAM
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		var kb int
		fmt.Sscanf(string(data), "MemTotal: %d", &kb)
		cfg.DetectedRAMMB = kb / 1024
	}

	data, err := os.ReadFile(mysqlCnfPath)
	if err != nil {
		return cfg, nil
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "innodb_buffer_pool_size") {
			var val int
			fmt.Sscanf(line, "innodb_buffer_pool_size = %dM", &val)
			if val > 0 {
				cfg.BufferPoolMB = val
			}
		} else if strings.HasPrefix(line, "max_connections") {
			fmt.Sscanf(line, "max_connections = %d", &cfg.MaxConnections)
		} else if strings.HasPrefix(line, "performance_schema") {
			cfg.PerfSchema = strings.Contains(line, "ON")
		}
	}

	return cfg, nil
}

func (s *Server) handleSetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	var cfg MySQLConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if cfg.BufferPoolMB < 16 || cfg.BufferPoolMB > 16384 {
		return nil, fmt.Errorf("buffer_pool_mb must be between 16 and 16384")
	}
	if cfg.MaxConnections < 5 || cfg.MaxConnections > 5000 {
		return nil, fmt.Errorf("max_connections must be between 5 and 5000")
	}

	perfSchema := "OFF"
	if cfg.PerfSchema {
		perfSchema = "ON"
	}

	cnf := fmt.Sprintf(`[mysqld]
# FastCP MySQL tuning
innodb_buffer_pool_size = %dM
innodb_log_file_size = 16M
innodb_log_buffer_size = 8M
innodb_flush_log_at_trx_commit = 2
innodb_flush_method = O_DIRECT
key_buffer_size = 4M
max_connections = %d
table_open_cache = 200
thread_cache_size = 8
performance_schema = %s
skip-name-resolve
`, cfg.BufferPoolMB, cfg.MaxConnections, perfSchema)

	os.MkdirAll("/etc/mysql/conf.d", 0755)
	if err := os.WriteFile(mysqlCnfPath, []byte(cnf), 0644); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	if err := s.restartMySQLService(); err != nil {
		return nil, fmt.Errorf("failed to restart MySQL: %w", err)
	}
	if err := s.applyMySQLRuntimeConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to apply MySQL runtime config: %w", err)
	}

	slog.Info("updated MySQL config", "buffer_pool_mb", cfg.BufferPoolMB, "max_connections", cfg.MaxConnections, "perf_schema", perfSchema)
	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleGetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	totalRAMMB := detectSystemRAMMB()
	return loadCaddyConfig(totalRAMMB), nil
}

func (s *Server) handleSetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	var req CaddyConfig
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	totalRAMMB := detectSystemRAMMB()
	cfg, err := normalizeAndValidateCaddyConfig(&req, totalRAMMB)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(caddyCfgPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create caddy settings directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal caddy settings: %w", err)
	}
	if err := os.WriteFile(caddyCfgPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write caddy settings: %w", err)
	}

	if err := s.generateCaddyfile(); err != nil {
		return nil, fmt.Errorf("failed to regenerate Caddyfile: %w", err)
	}
	if !s.isCaddyRunning() {
		if err := s.startCaddy(); err != nil {
			return nil, fmt.Errorf("failed to start Caddy: %w", err)
		}
	} else {
		if err := s.reloadCaddy(); err != nil {
			return nil, fmt.Errorf("failed to reload Caddy: %w", err)
		}
	}

	slog.Info("updated caddy performance config", "profile", cfg.Profile, "expert_mode", cfg.ExpertMode, "access_logs", cfg.AccessLogs)
	return map[string]string{"status": "ok"}, nil
}

func (s *Server) applyMySQLRuntimeConfig(cfg MySQLConfig) error {
	db, err := sql.Open("mysql", fmt.Sprintf("root@unix(%s)/", mysqlSocket))
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("SET GLOBAL max_connections = ?", cfg.MaxConnections); err != nil {
		return fmt.Errorf("set max_connections failed: %w", err)
	}

	var (
		name  string
		value string
	)
	if err := db.QueryRow("SHOW VARIABLES LIKE 'max_connections'").Scan(&name, &value); err != nil {
		return fmt.Errorf("read back max_connections failed: %w", err)
	}
	current, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid max_connections value returned: %q", value)
	}
	if current != cfg.MaxConnections {
		return fmt.Errorf("max_connections mismatch after apply: expected %d, got %d", cfg.MaxConnections, current)
	}

	return nil
}

func (s *Server) restartMySQLService() error {
	if s.hasSystemd() {
		var lastErr error
		for _, service := range []string{"mysql", "mysqld", "mariadb"} {
			if err := s.serviceReloadOrRestart(service); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return lastErr
		}
	}

	for _, service := range []string{"mysql", "mysqld", "mariadb"} {
		if output, err := exec.Command("service", service, "restart").CombinedOutput(); err == nil {
			return nil
		} else {
			slog.Debug("service restart failed", "service", service, "output", strings.TrimSpace(string(output)))
		}
	}

	return fmt.Errorf("failed to restart mysql service using known service names")
}

func parseSSHConfigFile(path string, cfg *SSHConfig) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	passwordAuth := cfg.PasswordAuth
	kbdAuth := cfg.PasswordAuth
	passwordSeen := false
	kbdSeen := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.ToLower(fields[0])
		val := strings.ToLower(fields[1])
		switch key {
		case "port":
			if port, err := strconv.Atoi(fields[1]); err == nil && port >= 1 && port <= 65535 {
				cfg.Port = port
			}
		case "passwordauthentication":
			passwordSeen = true
			passwordAuth = (val == "yes")
		case "kbdinteractiveauthentication", "challengeresponseauthentication":
			kbdSeen = true
			kbdAuth = (val == "yes")
		}
	}

	if passwordSeen || kbdSeen {
		cfg.PasswordAuth = passwordAuth || kbdAuth
	}
}

func resolveSSHDBinary() (string, error) {
	if p, err := exec.LookPath("sshd"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/sbin/sshd", "/sbin/sshd", "/usr/local/sbin/sshd"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("sshd binary not found")
}

func ensureSSHRuntimeDir() error {
	const sshRunDir = "/run/sshd"
	if err := os.MkdirAll(sshRunDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", sshRunDir, err)
	}
	return nil
}

func (s *Server) getEffectiveSSHConfig() (*SSHConfig, error) {
	cfg := &SSHConfig{
		Port:         22,
		PasswordAuth: true,
	}

	sshdPath, err := resolveSSHDBinary()
	if err != nil {
		return nil, err
	}
	if err := ensureSSHRuntimeDir(); err != nil {
		return nil, err
	}

	output, err := exec.Command(sshdPath, "-T", "-f", sshdMainConfig).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to read effective ssh config: %w: %s", err, strings.TrimSpace(string(output)))
	}

	passwordAuth := true
	kbdAuth := true
	passwordSeen := false
	kbdSeen := false

	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "port":
			if port, convErr := strconv.Atoi(fields[1]); convErr == nil && port >= 1 && port <= 65535 {
				cfg.Port = port
			}
		case "passwordauthentication":
			passwordSeen = true
			passwordAuth = strings.EqualFold(fields[1], "yes")
		case "kbdinteractiveauthentication", "challengeresponseauthentication":
			kbdSeen = true
			kbdAuth = strings.EqualFold(fields[1], "yes")
		}
	}
	if passwordSeen || kbdSeen {
		// Treat "password authentication" in UI as any password-based SSH auth path.
		cfg.PasswordAuth = passwordAuth || kbdAuth
	}

	return cfg, nil
}

func (s *Server) restartSSHService() error {
	if s.hasSystemd() {
		var lastErr error
		for _, service := range []string{"ssh", "sshd"} {
			if err := s.serviceReloadOrRestart(service); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return lastErr
		}
	}

	if output, err := exec.Command("service", "ssh", "restart").CombinedOutput(); err == nil {
		return nil
	} else {
		output2, err2 := exec.Command("service", "sshd", "restart").CombinedOutput()
		if err2 == nil {
			return nil
		}
		return fmt.Errorf("failed to restart SSH service: %s; %s", strings.TrimSpace(string(output)), strings.TrimSpace(string(output2)))
	}
}

func (s *Server) handleGetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	cfg, err := s.getEffectiveSSHConfig()
	if err == nil {
		return cfg, nil
	}
	slog.Warn("failed to read effective ssh config, falling back to file parse", "error", err)

	cfg = &SSHConfig{Port: 22, PasswordAuth: true}

	parseSSHConfigFile(sshdMainConfig, cfg)
	if files, globErr := filepath.Glob("/etc/ssh/sshd_config.d/*.conf"); globErr == nil {
		for _, f := range files {
			parseSSHConfigFile(f, cfg)
		}
	}
	parseSSHConfigFile(sshdFastcpConf, cfg)

	return cfg, nil
}

func restoreSSHFiles(backups map[string][]byte) {
	for path, content := range backups {
		_ = os.WriteFile(path, content, 0644)
	}
}

func ensureSSHDropInInclude() ([]byte, bool, error) {
	data, err := os.ReadFile(sshdMainConfig)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s: %w", sshdMainConfig, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Include") {
			continue
		}
		if strings.Contains(fields[1], "/etc/ssh/sshd_config.d/") {
			return nil, false, nil
		}
	}

	newContent := "Include /etc/ssh/sshd_config.d/*.conf\n" + string(data)
	if err := os.WriteFile(sshdMainConfig, []byte(newContent), 0644); err != nil {
		return nil, false, fmt.Errorf("failed to update %s include directives: %w", sshdMainConfig, err)
	}
	return data, true, nil
}

func disableConflictingSSHPortDirectives(targetPort int) (map[string][]byte, error) {
	files := []string{sshdMainConfig}
	if includeFiles, err := filepath.Glob("/etc/ssh/sshd_config.d/*.conf"); err == nil {
		for _, f := range includeFiles {
			if f == sshdFastcpConf {
				continue
			}
			files = append(files, f)
		}
	}

	backups := make(map[string][]byte)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		changed := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) < 2 || !strings.EqualFold(fields[0], "Port") {
				continue
			}
			port, convErr := strconv.Atoi(fields[1])
			if convErr != nil || port < 1 || port > 65535 || port == targetPort {
				continue
			}
			lines[i] = "# FastCP disabled conflicting Port directive: " + trimmed
			changed = true
		}

		if !changed {
			continue
		}
		backups[path] = data
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			restoreSSHFiles(backups)
			return nil, fmt.Errorf("failed to normalize ssh port directives in %s: %w", path, err)
		}
	}

	return backups, nil
}

func (s *Server) handleSetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	var cfg SSHConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}

	authValue := "yes"
	kbdInteractiveValue := "yes"
	if !cfg.PasswordAuth {
		authValue = "no"
		kbdInteractiveValue = "no"
	}

	content := fmt.Sprintf(`# Managed by FastCP
# Use the control panel to modify these values.
Port %d
PasswordAuthentication %s
KbdInteractiveAuthentication %s
`, cfg.Port, authValue, kbdInteractiveValue)

	_ = os.MkdirAll(filepath.Dir(sshdFastcpConf), 0755)

	previousContent, _ := os.ReadFile(sshdFastcpConf)
	mainConfigBackup, mainConfigChanged, err := ensureSSHDropInInclude()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(sshdFastcpConf, []byte(content), 0644); err != nil {
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		return nil, fmt.Errorf("failed to write ssh config: %w", err)
	}

	updatedFilesBackup, err := disableConflictingSSHPortDirectives(cfg.Port)
	if err != nil {
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		if len(previousContent) > 0 {
			_ = os.WriteFile(sshdFastcpConf, previousContent, 0644)
		} else {
			_ = os.Remove(sshdFastcpConf)
		}
		return nil, err
	}

	sshdPath, err := resolveSSHDBinary()
	if err != nil {
		restoreSSHFiles(updatedFilesBackup)
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		if len(previousContent) > 0 {
			_ = os.WriteFile(sshdFastcpConf, previousContent, 0644)
		} else {
			_ = os.Remove(sshdFastcpConf)
		}
		return nil, fmt.Errorf("ssh config validation failed: %w", err)
	}
	if err := ensureSSHRuntimeDir(); err != nil {
		restoreSSHFiles(updatedFilesBackup)
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		if len(previousContent) > 0 {
			_ = os.WriteFile(sshdFastcpConf, previousContent, 0644)
		} else {
			_ = os.Remove(sshdFastcpConf)
		}
		return nil, fmt.Errorf("ssh config validation failed: %w", err)
	}

	if output, err := exec.Command(sshdPath, "-t", "-f", sshdMainConfig).CombinedOutput(); err != nil {
		restoreSSHFiles(updatedFilesBackup)
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		if len(previousContent) > 0 {
			_ = os.WriteFile(sshdFastcpConf, previousContent, 0644)
		} else {
			_ = os.Remove(sshdFastcpConf)
		}
		return nil, fmt.Errorf("ssh config validation failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	if err := s.restartSSHService(); err != nil {
		restoreSSHFiles(updatedFilesBackup)
		if mainConfigChanged {
			_ = os.WriteFile(sshdMainConfig, mainConfigBackup, 0644)
		}
		if len(previousContent) > 0 {
			_ = os.WriteFile(sshdFastcpConf, previousContent, 0644)
		} else {
			_ = os.Remove(sshdFastcpConf)
		}
		return nil, fmt.Errorf("failed to apply SSH settings: %w", err)
	}

	slog.Info("updated SSH config", "port", cfg.Port, "password_auth", cfg.PasswordAuth)
	return map[string]string{"status": "ok"}, nil
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

	// Ensure updater-based installations also receive backup tooling
	// introduced after initial install (e.g. restic/rsync).
	if err := s.installMissingBackupDependencies(); err != nil {
		return nil, err
	}

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

	// Bootstrap all directories and fix ownership
	bootstrapUserEnvironment(req.Username)

	u, _ := user.Lookup(req.Username)
	uid, _ := strconv.Atoi(u.Uid)

	// Create user's Caddyfile (initially empty, will be populated when sites are created)
	userConfigDir := filepath.Join("/opt/fastcp/config/users", req.Username)
	userCaddyfile := filepath.Join(userConfigDir, "Caddyfile")
	initialCaddyfile := fmt.Sprintf(`# FastCP user config for %s
{
    admin off
}

# Sites will be added here by FastCP
`, req.Username)
	os.WriteFile(userCaddyfile, []byte(initialCaddyfile), 0644)

	// Create systemd user slice with resource limits (applies to ALL user processes)
	if err := s.createUserResourceSlice(req.Username, uid, req.MemoryMB, req.CPUPercent); err != nil {
		slog.Warn("failed to create user resource slice", "error", err)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleUpdateUserLimits(ctx context.Context, params json.RawMessage) (any, error) {
	var req UpdateUserLimitsRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.MemoryMB != -1 && (req.MemoryMB < 128 || req.MemoryMB > 262144) {
		return nil, fmt.Errorf("memory_mb must be -1 (unlimited) or between 128 and 262144")
	}
	if req.CPUPercent != -1 && (req.CPUPercent < 10 || req.CPUPercent > 4000) {
		return nil, fmt.Errorf("cpu_percent must be -1 (unlimited) or between 10 and 4000")
	}

	u, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	if err := s.createUserResourceSlice(req.Username, uid, req.MemoryMB, req.CPUPercent); err != nil {
		return nil, fmt.Errorf("failed to update user resource limits: %w", err)
	}

	slog.Info("updated user resource limits", "username", req.Username, "memory_mb", req.MemoryMB, "cpu_percent", req.CPUPercent)
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

	memoryDirective := fmt.Sprintf("MemoryMax=%dM", memoryMB)
	cpuDirective := fmt.Sprintf("CPUQuota=%d%%", cpuPercent)
	if memoryMB < 0 {
		memoryDirective = "MemoryMax=infinity"
	}
	if cpuPercent < 0 {
		cpuDirective = "CPUQuota=infinity"
	}

	// Create override file with resource limits
	overrideContent := fmt.Sprintf(`# FastCP resource limits for user: %s (UID: %d)
# These limits apply to ALL processes by this user:
# - PHP-FPM pools for this user
# - SSH sessions
# - Cron jobs
# - Any other processes

[Slice]
%s
%s
`, username, uid, memoryDirective, cpuDirective)

	overridePath := filepath.Join(sliceDir, "50-fastcp-limits.conf")
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		return fmt.Errorf("failed to write slice override: %w", err)
	}

	// Reload systemd to apply changes
	exec.Command("systemctl", "daemon-reload").Run()

	slog.Info("created user resource slice", "username", username, "uid", uid, "memory_mb", memoryMB, "cpu_percent", cpuPercent)
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
	if err := s.reloadCaddy(); err != nil {
		slog.Warn("failed to reload Caddy after user delete", "error", err)
	}

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
