//go:build linux

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/rehmatworks/fastcp/internal/version"
)

// ---------------------------------------------------------------------------
// Web server settings

func loadCaddySettings(ram int) CaddyConfig {
	data, err := os.ReadFile(CaddySettingsPath)
	if err != nil {
		return defaultCaddyConfig(ram)
	}
	var raw struct {
		CaddyConfig
		ExpertMode bool `json:"expert_mode"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("ignoring unreadable web server settings", "error", err)
		return defaultCaddyConfig(ram)
	}
	cfg := raw.CaddyConfig
	if raw.ExpertMode {
		cfg.Profile = "custom"
	}
	out, err := normalizeCaddyConfig(cfg, ram)
	if err != nil {
		slog.Warn("ignoring invalid web server settings", "error", err)
		return defaultCaddyConfig(ram)
	}
	return out
}

func (a *Agent) handleGetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	cfg := loadCaddySettings(systemRAMMB())
	return &cfg, nil
}

func (a *Agent) handleSetCaddyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[CaddyConfig](params)
	if err != nil {
		return nil, err
	}
	cfg, err := normalizeCaddyConfig(*req, systemRAMMB())
	if err != nil {
		return nil, invalid(err)
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	old, readErr := os.ReadFile(CaddySettingsPath)
	if err := writeFileAtomic(CaddySettingsPath, data, 0o644); err != nil {
		return nil, err
	}
	if err := a.reapply(ctx); err != nil {
		if readErr == nil {
			_ = writeFileAtomic(CaddySettingsPath, old, 0o644)
		} else {
			_ = os.Remove(CaddySettingsPath)
		}
		return nil, err
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Status

type versionCache struct {
	mu     sync.Mutex
	at     time.Time
	mysql  string
	caddy  string
	osName string
	kernel string
}

var versions versionCache

func (c *versionCache) get(ctx context.Context) (mysql, caddy, osName, kernel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) > 5*time.Minute {
		c.mysql = mysqlVersion(ctx)
		c.caddy = caddyVersion(ctx)
		c.osName = osPrettyName()
		var u unix.Utsname
		if unix.Uname(&u) == nil {
			c.kernel = unix.ByteSliceToString(u.Release[:])
		}
		c.at = time.Now()
	}
	return c.mysql, c.caddy, c.osName, c.kernel
}

func (c *versionCache) invalidate() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}

func caddyVersion(ctx context.Context) string {
	out, err := run(ctx, 10*time.Second, CaddyBinary, "version")
	if err != nil {
		return ""
	}
	return parseCaddyVersion(out)
}

func osPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return runtime.GOOS
}

func meminfo() map[string]uint64 {
	out := map[string]uint64{}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			if v, err := strconv.ParseUint(f[1], 10, 64); err == nil {
				out[strings.TrimSuffix(f[0], ":")] = v * 1024
			}
		}
	}
	return out
}

func (a *Agent) handleSystemStatus(ctx context.Context, params json.RawMessage) (any, error) {
	st := &SystemStatus{
		Architecture: runtime.GOARCH,
		CPUCount:     runtime.NumCPU(),
		AgentVersion: version.Version,
		PHPVersions:  sortedKeys(installedPHPBranches()),
	}
	st.Hostname, _ = os.Hostname()
	st.MySQLVersion, st.CaddyVersion, st.OS, st.Kernel = versions.get(ctx)
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(data)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				st.Uptime = int64(v)
			}
		}
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		for i := 0; i < 3 && i < len(f); i++ {
			st.LoadAverage[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	m := meminfo()
	st.MemoryTotal = m["MemTotal"]
	if avail, ok := m["MemAvailable"]; ok && avail <= st.MemoryTotal {
		st.MemoryUsed = st.MemoryTotal - avail
	}
	st.SwapTotal = m["SwapTotal"]
	if m["SwapFree"] <= st.SwapTotal {
		st.SwapUsed = st.SwapTotal - m["SwapFree"]
	}
	var fs unix.Statfs_t
	if unix.Statfs("/", &fs) == nil {
		bs := uint64(fs.Bsize)
		st.DiskTotal = fs.Blocks * bs
		st.DiskUsed = (fs.Blocks - fs.Bfree) * bs
	}
	return st, nil
}

func (a *Agent) serviceUnits() []string {
	units := []string{"fastcp.service", "fastcp-agent.service", "fastcp-caddy.service"}
	for _, svc := range []string{"mysql", "mariadb"} {
		if a.unitExists(svc + ".service") {
			units = append(units, svc+".service")
			break
		}
	}
	for _, v := range sortedKeys(installedPHPBranches()) {
		units = append(units, fpmService(v)+".service")
	}
	for _, svc := range []string{"ssh", "cron"} {
		if a.unitExists(svc + ".service") {
			units = append(units, svc+".service")
		}
	}
	return units
}

func (a *Agent) handleServices(ctx context.Context, params json.RawMessage) (any, error) {
	if !a.systemd {
		return []ServiceStatus{}, nil
	}
	units := a.serviceUnits()
	args := append([]string{"show", "--property=Id,Description,ActiveState,SubState,UnitFileState,LoadState"}, units...)
	out, err := run(ctx, 15*time.Second, "systemctl", args...)
	if err != nil {
		return nil, err
	}
	var list []ServiceStatus
	for _, block := range strings.Split(out, "\n\n") {
		props := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				props[k] = v
			}
		}
		if props["Id"] == "" || props["LoadState"] == "not-found" {
			continue
		}
		list = append(list, ServiceStatus{
			Name:        strings.TrimSuffix(props["Id"], ".service"),
			Description: props["Description"],
			Active:      props["ActiveState"] == "active",
			State:       props["SubState"],
			Enabled:     props["UnitFileState"] == "enabled",
		})
	}
	return list, nil
}

// ---------------------------------------------------------------------------
// rclone

func rcloneStatus(ctx context.Context) *ToolStatus {
	path, err := exec.LookPath("rclone")
	if err != nil {
		return &ToolStatus{}
	}
	st := &ToolStatus{Installed: true}
	if out, err := run(ctx, 10*time.Second, path, "version"); err == nil {
		first, _, _ := strings.Cut(out, "\n")
		st.Version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(first), "rclone "), "v")
	}
	return st
}

func (a *Agent) handleRcloneStatus(ctx context.Context, params json.RawMessage) (any, error) {
	return rcloneStatus(ctx), nil
}

func (a *Agent) handleRcloneInstall(ctx context.Context, params json.RawMessage) (any, error) {
	return a.jobs.Start(a.ctx, "rclone.install", "rclone", func(ctx context.Context, log *JobLog) (string, error) {
		if rcloneStatus(ctx).Installed {
			return "rclone is already installed.", nil
		}
		if err := a.apt(ctx, log, 5*time.Minute, "update"); err != nil {
			return "", err
		}
		if err := a.apt(ctx, log, 15*time.Minute, "install", "--no-install-recommends", "rclone"); err != nil {
			return "", err
		}
		st := rcloneStatus(ctx)
		if !st.Installed {
			return "", fmt.Errorf("rclone was installed but cannot be found")
		}
		return "Installed rclone " + st.Version + ".", nil
	}), nil
}

func (a *Agent) handleJobGet(ctx context.Context, params json.RawMessage) (any, error) {
	a.syncUpdateResult()
	return a.jobs.handleGet(ctx, params)
}

// ---------------------------------------------------------------------------
// Login message

const motdScript = `#!/bin/sh
# FastCP login message. Managed by FastCP.
version="$(cat /opt/fastcp/config/motd-version 2>/dev/null)"
ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "$ip" ] || ip="your-server-ip"
printf '\n  FastCP %s\n  Control panel: https://%s:2050\n\n' "${version:-}" "$ip"
`

// ensureMOTD installs the login message. The script only reads a version
// file; it never runs FastCP binaries as the logging-in user.
func ensureMOTD() {
	if _, err := os.Stat("/etc/update-motd.d"); err != nil {
		return
	}
	if _, err := writeIfChanged(MOTDScript, []byte(motdScript), 0o755); err != nil {
		slog.Warn("could not install login message", "error", err)
	}
	_, _ = writeIfChanged(MOTDVersionFile, []byte(version.Version+"\n"), 0o644)
}
