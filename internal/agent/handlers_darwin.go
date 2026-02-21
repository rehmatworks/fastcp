//go:build darwin

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	homeBase      = "/Users"
	appsDir       = "apps"
	fastcpDir     = ".fastcp"
	wpDownloadURL = "https://wordpress.org/latest.tar.gz"
	caddyConfig   = "/usr/local/etc/fastcp/Caddyfile"
	mysqlSocket   = "/tmp/mysql.sock"
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

	return map[string]string{"status": "ok", "path": siteDir}, nil
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
		OS:           "macOS",
		Uptime:       0,
		LoadAverage:  loadAvg,
		MemoryTotal:  memTotal,
		MemoryUsed:   memUsed,
		DiskTotal:    0,
		DiskUsed:     0,
		PHPVersion:   phpVersion,
		MySQLVersion: mysqlVersion,
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

func (s *Server) handleSystemUpdate(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("system updates not supported on macOS - use Ubuntu for production")
}

func (s *Server) handleSyncCronJobs(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("cron job sync not supported on macOS - use Ubuntu for production")
}

func (s *Server) runStartupMigrations() {}

func (s *Server) handleGetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return &MySQLConfig{BufferPoolMB: 128, MaxConnections: 151, PerfSchema: true, DetectedRAMMB: 0}, nil
}

func (s *Server) handleSetMySQLConfig(ctx context.Context, params json.RawMessage) (any, error) {
	return nil, fmt.Errorf("MySQL config not supported on macOS - use Ubuntu for production")
}
