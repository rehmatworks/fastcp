//go:build darwin

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	homeBase         = "/Users"
	appsDir          = "apps"
	fastcpDir        = ".fastcp"
	wpDownloadURL    = "https://wordpress.org/latest.tar.gz"
	caddyConfig      = "/usr/local/etc/fastcp/Caddyfile"
	mysqlSocket      = "/tmp/mysql.sock"
	controlPanelPort = 2050
)

// macOS-specific implementations

func (s *Server) handleCreateSiteDirectory(ctx context.Context, params json.RawMessage) (any, error) {
	var req CreateSiteDirectoryRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = filepath.Join(homeBase, req.Username)
	}

	siteDir := filepath.Join(homeDir, appsDir, req.Slug)
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
		return nil, fmt.Errorf("failed to copy default site logo: %w", err)
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

	homeDir, _ := os.UserHomeDir()
	siteDir := filepath.Join(homeDir, appsDir, req.Slug)

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

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("wp-%s.tar.gz", req.Username))
	if err := exec.Command("curl", "-sL", "-o", tmpFile, wpDownloadURL).Run(); err != nil {
		return nil, fmt.Errorf("failed to download WordPress: %w", err)
	}
	defer os.Remove(tmpFile)

	publicDir := req.Path
	if err := exec.Command("tar", "-xzf", tmpFile, "-C", filepath.Dir(publicDir), "--strip-components=1").Run(); err != nil {
		return nil, fmt.Errorf("failed to extract WordPress: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleReloadCaddy(ctx context.Context, params json.RawMessage) (any, error) {
	// On macOS, try to reload via admin API
	cmd := exec.Command("curl", "-s", "-X", "POST", "http://localhost:2019/load",
		"-H", "Content-Type: text/caddyfile",
		"--data-binary", fmt.Sprintf("@%s", caddyConfig))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to reload Caddy: %w: %s", err, output)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleCreateDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	// macOS development stub - just pretend it worked
	return map[string]string{"status": "ok", "note": "macOS dev mode - database not actually created"}, nil
}

func (s *Server) handleDeleteDatabase(ctx context.Context, params json.RawMessage) (any, error) {
	return map[string]string{"status": "ok", "note": "macOS dev mode"}, nil
}

func (s *Server) handleResetDatabasePassword(ctx context.Context, params json.RawMessage) (any, error) {
	return map[string]string{"status": "ok", "note": "macOS dev mode - password rotation not actually performed"}, nil
}

func (s *Server) handleAddSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	var req AddSSHKeyRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	os.MkdirAll(sshDir, 0700)

	authKeysPath := filepath.Join(sshDir, "authorized_keys")
	f, err := os.OpenFile(authKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open authorized_keys: %w", err)
	}
	defer f.Close()

	keyLine := fmt.Sprintf("%s # fastcp:%s:%s\n", strings.TrimSpace(req.PublicKey), req.KeyID, req.Name)
	if _, err := f.WriteString(keyLine); err != nil {
		return nil, fmt.Errorf("failed to write key: %w", err)
	}

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleRemoveSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	var req RemoveSSHKeyRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	authKeysPath := filepath.Join(homeDir, ".ssh", "authorized_keys")

	content, err := os.ReadFile(authKeysPath)
	if err != nil {
		return map[string]string{"status": "ok"}, nil
	}

	marker := fmt.Sprintf("fastcp:%s:", req.KeyID)
	var newLines []string
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.Contains(line, marker) && line != "" {
			newLines = append(newLines, line)
		}
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}

	os.WriteFile(authKeysPath, []byte(newContent), 0600)

	return map[string]string{"status": "ok"}, nil
}

func (s *Server) handleSystemStatus(ctx context.Context, params json.RawMessage) (any, error) {
	hostname, _ := os.Hostname()

	// Get load average via sysctl on macOS
	loadAvg := 0.0
	if output, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		fmt.Sscanf(string(output), "{ %f", &loadAvg)
	}

	// Get memory info via sysctl
	var memTotal, memUsed uint64
	if output, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &memTotal)
	}

	availablePHPVersions := make([]string, 0, 4)
	for _, v := range []string{"8.2", "8.3", "8.4", "8.5"} {
		if err := exec.Command("php"+v, "-v").Run(); err == nil {
			availablePHPVersions = append(availablePHPVersions, v)
		}
	}

	// FastCP default PHP is 8.4. Prefer reporting that when installed.
	phpVersion := ""
	for _, v := range availablePHPVersions {
		if v == "8.4" {
			phpVersion = "8.4"
			break
		}
	}
	if phpVersion == "" {
		if output, err := exec.Command("php", "-v").Output(); err == nil {
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 {
				parts := strings.Fields(lines[0])
				if len(parts) >= 2 {
					phpVersion = parts[1]
				}
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

	// Get Caddy version
	caddyVersion := ""
	if output, err := exec.Command("caddy", "version").Output(); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(output)))
		if len(fields) > 0 {
			caddyVersion = fields[0]
		}
	}

	kernelVersion := ""
	if output, err := exec.Command("uname", "-r").Output(); err == nil {
		kernelVersion = strings.TrimSpace(string(output))
	}

	architecture := ""
	if output, err := exec.Command("uname", "-m").Output(); err == nil {
		architecture = strings.TrimSpace(string(output))
	}

	return &SystemStatus{
		Hostname:             hostname,
		OS:                   "macOS",
		Uptime:               0,
		LoadAverage:          loadAvg,
		MemoryTotal:          memTotal,
		MemoryUsed:           memUsed,
		DiskTotal:            0,
		DiskUsed:             0,
		PHPVersion:           phpVersion,
		MySQLVersion:         mysqlVersion,
		CaddyVersion:         caddyVersion,
		PHPAvailableVersions: availablePHPVersions,
		KernelVersion:        kernelVersion,
		Architecture:         architecture,
		TotalUsers:           0,
		TotalWebsites:        0,
		TotalDatabases:       0,
	}, nil
}

func (s *Server) handleSystemServices(ctx context.Context, params json.RawMessage) (any, error) {
	// macOS doesn't use systemd, return mock data for development
	return []*ServiceStatus{
		{Name: "fastcp", Status: "running", Enabled: true},
		{Name: "fastcp-agent", Status: "running", Enabled: true},
		{Name: "mysql", Status: "unknown", Enabled: false},
	}, nil
}

func (s *Server) handleCreateUser(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("user creation not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleDeleteUser(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("user deletion not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleUpdateUserLimits(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("user limit updates not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleSystemUpdate(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("system updates not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleSyncCronJobs(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("cron job sync not supported on macOS - use Ubuntu for production")
}

func (s *Server) runStartupMigrations() {}

func (s *Server) isCaddyRunning() bool { return false }
func (s *Server) startCaddy() error    { return nil }
func (s *Server) reloadCaddy()         {}
func (s *Server) ensureCaddyBinary()   {}

func (s *Server) handleGetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return &MySQLConfig{BufferPoolMB: 128, MaxConnections: 30, PerfSchema: false, DetectedRAMMB: 0}, nil
}

func (s *Server) handleSetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("MySQL config not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleGetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return &SSHConfig{Port: 22, PasswordAuth: true}, nil
}

func (s *Server) handleSetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("SSH config not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleGetPHPDefaultConfig(ctx context.Context, params json.RawMessage) (any, error) {
	availablePHPVersions := make([]string, 0, 4)
	for _, v := range []string{"8.2", "8.3", "8.4", "8.5"} {
		if err := exec.Command("php"+v, "-v").Run(); err == nil {
			availablePHPVersions = append(availablePHPVersions, v)
		}
	}
	defaultVersion := "8.4"
	if len(availablePHPVersions) > 0 {
		found84 := false
		for _, v := range availablePHPVersions {
			if v == "8.4" {
				found84 = true
				break
			}
		}
		if !found84 {
			defaultVersion = availablePHPVersions[0]
		}
	}
	return &PHPDefaultConfig{
		DefaultPHPVersion:    defaultVersion,
		AvailablePHPVersions: availablePHPVersions,
	}, nil
}

func (s *Server) handleSetPHPDefaultConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("PHP default setting is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleInstallPHPVersion(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("PHP runtime install is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleGetFirewallStatus(ctx context.Context, params json.RawMessage) (any, error) {
	return &FirewallStatus{
		Installed:        false,
		Enabled:          false,
		ControlPanelPort: 2050,
		Rules:            []FirewallRule{},
	}, nil
}

func (s *Server) handleInstallFirewall(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("UFW management is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleSetFirewallEnabled(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("UFW management is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleFirewallAllowPort(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("UFW management is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleFirewallDenyPort(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("UFW management is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleFirewallDeleteRule(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("UFW management is not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleGetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return &CaddyConfig{
		Profile:       "low_ram",
		AccessLogs:    false,
		ExpertMode:    false,
		ReadHeader:    "8s",
		ReadBody:      "20s",
		WriteTimeout:  "45s",
		IdleTimeout:   "45s",
		GracePeriod:   "5s",
		MaxHeaderSize: 16384,
	}, nil
}

func (s *Server) handleSetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("Caddy config tuning not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleGetRcloneStatus(ctx context.Context, params json.RawMessage) (any, error) {
	return &RcloneStatus{
		Installed: false,
	}, nil
}

func (s *Server) handleInstallRclone(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("rclone install is not supported on macOS - use Ubuntu for production")
}
