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

	safeDomain := strings.ReplaceAll(req.Domain, ".", "_")
	siteDir := filepath.Join(homeDir, appsDir, safeDomain)
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

	// Create default index.php
	indexPath := filepath.Join(siteDir, "public", "index.php")
	indexContent := fmt.Sprintf(`<?php
echo "<h1>Welcome to %s</h1>";
echo "<p>Your site is ready. Upload your files to get started.</p>";
phpinfo();
`, req.Domain)

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
	safeDomain := strings.ReplaceAll(req.Domain, ".", "_")
	siteDir := filepath.Join(homeDir, appsDir, safeDomain)

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
