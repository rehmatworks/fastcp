//go:build linux

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (a *Agent) loadState() {
	data, err := os.ReadFile(StatePath)
	if err != nil {
		return
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		slog.Warn("ignoring unreadable state file", "path", StatePath, "error", err)
		return
	}
	a.stateMu.Lock()
	a.state = &st
	a.stateMu.Unlock()
}

func (a *Agent) saveState(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(StatePath, data, 0o600)
}

// applyCachedState regenerates configuration from the last pushed state so
// sites keep working after an agent restart, before the panel reconnects.
func (a *Agent) applyCachedState() {
	a.stateMu.RLock()
	st := a.state
	a.stateMu.RUnlock()
	if st == nil {
		return
	}
	res, err := a.apply(a.ctx, st, false)
	if err != nil {
		slog.Error("re-applying cached state failed", "error", err)
		return
	}
	for _, w := range res.Warnings {
		slog.Warn("config warning", "warning", w)
	}
}

func (a *Agent) handleApply(ctx context.Context, params json.RawMessage) (any, error) {
	st, err := decode[State](params)
	if err != nil {
		return nil, err
	}
	return a.apply(ctx, st, true)
}

// apply renders and activates st. With save it also becomes the cached state.
func (a *Agent) apply(ctx context.Context, st *State, save bool) (*ApplyResult, error) {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	prev := a.currentState()
	installed := installedPHPBranches()
	settings := loadPHPSettings()
	def := effectiveDefaultPHP(settings, installed)
	p := buildPlan(st, lookupTenant, installed, def)

	for name, t := range p.Tenants {
		if a.isBootstrapped(name) {
			continue
		}
		if err := bootstrapTenant(t); err != nil {
			p.warnf("user %s: %v", name, err)
			continue
		}
		a.setBootstrapped(name, true)
	}
	for _, s := range p.Sites {
		if err := ensureTenantRuntime(s.Tenant); err != nil {
			p.warnf("site %s/%s: %v", s.Username, s.Slug, err)
		}
		if err := ensureSiteLogDir(s.Tenant, s.Slug); err != nil {
			p.warnf("site %s/%s: log directory: %v", s.Username, s.Slug, err)
		}
	}

	ram := systemRAMMB()
	pools := desiredPools(p, ram, def, installed)
	finishPools, err := a.syncPools(ctx, pools)
	if err != nil {
		return nil, err
	}

	cfg := loadCaddySettings(ram)
	_, caddy, _, _ := versions.get(ctx)
	opts := renderOptions{Dev: a.dev, Admin: "unix/" + CaddyAdminSocket, OwnerFS: caddy == CaddyBuild}
	if _, ok := pools[pmaPoolKey]; ok {
		opts.PMASocket = pmaSocketPath()
	}
	if err := a.syncCaddy(ctx, renderCaddyfile(p, cfg, opts)); err != nil {
		return nil, err
	}
	for _, w := range finishPools(ctx) {
		p.warnf("%s", w)
	}

	p.Warnings = append(p.Warnings, a.syncLimits(ctx, p, prev)...)
	p.Warnings = append(p.Warnings, a.syncAccounts(ctx, p, prev)...)

	a.stateMu.Lock()
	a.state = st
	a.stateMu.Unlock()
	if save {
		if err := a.saveState(st); err != nil {
			p.warnf("could not save state: %v", err)
		}
	}
	return &ApplyResult{Warnings: p.Warnings}, nil
}

// reapply renders the current state again, e.g. after PHP or Caddy
// settings changed.
// Before the panel has pushed any state this is a no-op, so the existing
// configuration is never replaced by an empty one.
func (a *Agent) reapply(ctx context.Context) error {
	a.stateMu.RLock()
	st := a.state
	a.stateMu.RUnlock()
	if st == nil {
		return nil
	}
	_, err := a.apply(ctx, st, false)
	return err
}

// ---------------------------------------------------------------------------
// PHP-FPM pools

const pmaPoolKey = "phpmyadmin"

type poolFile struct {
	Version string
	Path    string
	Content string
	Socket  string
}

func desiredPools(p *plan, ram int, def string, installed map[string]bool) map[string]poolFile {
	out := map[string]poolFile{}
	counts := p.tenantSiteCounts()
	for _, s := range p.Sites {
		if s.Suspended || s.PHPVersion == "" {
			continue
		}
		limit := p.Users[s.Username].MemoryMB
		pf := poolFile{
			Version: s.PHPVersion,
			Path:    sitePoolFile(s.PHPVersion, s.PoolName),
			Content: renderSitePool(s, sitePoolTuning(ram, limit, counts[s.Username])),
			Socket:  s.Socket,
		}
		out[pf.Path] = pf
	}
	if installed[def] && pmaInstalled() {
		out[pmaPoolKey] = poolFile{
			Version: def,
			Path:    sitePoolFile(def, "fastcp-phpmyadmin"),
			Content: renderPMAPool(ram),
			Socket:  pmaSocketPath(),
		}
	}
	return out
}

func pmaInstalled() bool {
	_, err := os.Stat(filepath.Join(PMADir, "index.php"))
	return err == nil
}

func existingPoolFiles() map[string]string {
	out := map[string]string{}
	for _, pattern := range []string{"/etc/php/*/fpm/pool.d/fastcp-site-*.conf", "/etc/php/*/fpm/pool.d/fastcp-phpmyadmin.conf"} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			version := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(m))))
			out[m] = version
		}
	}
	return out
}

// syncPools writes new and changed pools, tests and reloads the affected
// PHP versions and waits for their sockets. Stale pools are removed by the
// returned function after Caddy has switched away from them.
func (a *Agent) syncPools(ctx context.Context, desired map[string]poolFile) (func(context.Context) []string, error) {
	type backup struct {
		existed bool
		data    []byte
	}
	backups := map[string]backup{}
	changed := map[string]bool{}
	byPath := map[string]poolFile{}
	for _, pf := range desired {
		byPath[pf.Path] = pf
		old, err := os.ReadFile(pf.Path)
		if err == nil && string(old) == pf.Content {
			continue
		}
		backups[pf.Path] = backup{existed: err == nil, data: old}
		if err := os.MkdirAll(filepath.Dir(pf.Path), 0o755); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(pf.Path, []byte(pf.Content), 0o644); err != nil {
			return nil, err
		}
		changed[pf.Version] = true
	}
	// Unchanged pools still need a running php-fpm, which nothing else
	// starts after a container restart.
	for _, pf := range desired {
		if !changed[pf.Version] && !socketReady(pf.Socket) {
			changed[pf.Version] = true
		}
	}
	rollback := func() {
		for path, b := range backups {
			if b.existed {
				_ = writeFileAtomic(path, b.data, 0o644)
			} else {
				_ = os.Remove(path)
			}
		}
	}
	for _, v := range sortedKeys(changed) {
		if out, err := fpmTest(ctx, v); err != nil {
			rollback()
			return nil, fmt.Errorf("PHP %s configuration test failed, nothing was changed%s", v, tail(out, 1500))
		}
	}
	for _, v := range sortedKeys(changed) {
		var sockets []string
		for _, pf := range desired {
			if pf.Version == v {
				sockets = append(sockets, pf.Socket)
			}
		}
		if err := a.reloadFPM(ctx, v, sockets); err != nil {
			return nil, err
		}
	}

	return func(ctx context.Context) []string {
		var warnings []string
		stale := map[string]bool{}
		for path, version := range existingPoolFiles() {
			if _, keep := byPath[path]; keep {
				continue
			}
			if err := os.Remove(path); err != nil {
				warnings = append(warnings, fmt.Sprintf("could not remove stale pool %s: %v", path, err))
				continue
			}
			stale[version] = true
		}
		for _, v := range sortedKeys(stale) {
			if out, err := fpmTest(ctx, v); err != nil {
				warnings = append(warnings, fmt.Sprintf("PHP %s configuration test failed after removing pools%s", v, tail(out, 500)))
				continue
			}
			if err := a.reloadFPM(ctx, v, nil); err != nil {
				warnings = append(warnings, err.Error())
			}
		}
		removeStaleSockets(desired)
		return warnings
	}, nil
}

func fpmBinary(version string) string {
	for _, c := range []string{"/usr/sbin/php-fpm" + version, "/usr/sbin/php" + version + "-fpm"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func fpmService(version string) string { return "php" + version + "-fpm" }

func fpmTest(ctx context.Context, version string) (string, error) {
	bin := fpmBinary(version)
	if bin == "" {
		return "", fmt.Errorf("php-fpm %s is not installed", version)
	}
	return run(ctx, time.Minute, bin, "-t")
}

// reloadFPM gracefully reloads one PHP version (starting it when stopped)
// and waits until every expected socket accepts connections. A restart is
// attempted once if a reload left sockets missing.
func (a *Agent) reloadFPM(ctx context.Context, version string, sockets []string) error {
	svc := fpmService(version)
	if a.systemd && a.unitExists(svc) {
		_ = a.systemctl(ctx, "enable", "--quiet", svc)
		if err := a.systemctl(ctx, "reload-or-restart", svc); err != nil {
			return fmt.Errorf("reloading PHP %s failed: %w", version, err)
		}
		if waitSockets(sockets, 15*time.Second) {
			return nil
		}
		if err := a.systemctl(ctx, "restart", svc); err != nil {
			return fmt.Errorf("restarting PHP %s failed: %w", version, err)
		}
	} else if err := directFPMReload(ctx, version); err != nil {
		return err
	}
	if !waitSockets(sockets, 20*time.Second) {
		return fmt.Errorf("PHP %s did not open its sockets: %s", version, strings.Join(missingSockets(sockets), ", "))
	}
	return nil
}

// directFPMReload manages php-fpm without systemd (development containers).
func directFPMReload(ctx context.Context, version string) error {
	bin := fpmBinary(version)
	if bin == "" {
		return fmt.Errorf("php-fpm %s is not installed", version)
	}
	if pid := fpmMasterPID(version); pid > 1 && syscall.Kill(pid, syscall.SIGUSR2) == nil {
		return nil
	}
	_ = os.MkdirAll("/run/php", 0o755)
	_, err := run(ctx, time.Minute, bin, "--daemonize", "--pid", fpmPIDFile(version))
	return err
}

func fpmPIDFile(version string) string { return "/run/php/php" + version + "-fpm.pid" }

// fpmMasterPID finds the running php-fpm master for a version. Newer
// packages no longer set a PID file, so /proc is searched as a fallback.
func fpmMasterPID(version string) int {
	bin, err := filepath.EvalSymlinks(fpmBinary(version))
	if err != nil {
		return 0
	}
	// A PID file can outlive its process (containers keep /run across
	// restarts), so every candidate is checked against /proc.
	isMaster := func(pid int) bool {
		dir := filepath.Join("/proc", strconv.Itoa(pid))
		if exe, err := os.Readlink(filepath.Join(dir, "exe")); err != nil || exe != bin {
			return false
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		return err == nil && bytes.HasPrefix(cmdline, []byte("php-fpm: master process"))
	}
	if data, err := os.ReadFile(fpmPIDFile(version)); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 && isMaster(pid) {
			return pid
		}
	}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 1 && isMaster(pid) {
			return pid
		}
	}
	return 0
}

func socketReady(path string) bool {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func waitSockets(paths []string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if len(missingSockets(paths)) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func missingSockets(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !socketReady(p) {
			out = append(out, p)
		}
	}
	return out
}

// removeStaleSockets deletes leftover socket files nobody listens on.
func removeStaleSockets(desired map[string]poolFile) {
	keep := map[string]bool{}
	for _, pf := range desired {
		keep[pf.Socket] = true
	}
	matches, _ := filepath.Glob(filepath.Join(PHPSocketRoot, "*", "*.sock"))
	for _, m := range matches {
		if !keep[m] && !socketReady(m) {
			os.Remove(m)
		}
	}
}

// ---------------------------------------------------------------------------
// Caddy

const capNetBindService = 10

func caddyEnv() []string {
	return []string{
		"HOME=" + CaddyStorageDir,
		"XDG_DATA_HOME=" + filepath.Dir(CaddyStorageDir),
		"XDG_CONFIG_HOME=" + filepath.Join(CaddyStorageDir, ".config"),
	}
}

// caddyValidate checks a Caddyfile as the caddy user, so files it creates
// while provisioning (logs, storage) are not owned by root.
func caddyValidate(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, CaddyBinary, "validate", "--config", path, "--adapter", "caddyfile")
	cmd.Env = append([]string{"PATH=/usr/local/bin:/usr/bin:/bin"}, caddyEnv()...)
	cmd.Dir = "/"
	if uid, gid := caddyIDs(); uid > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}}
	}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return caddyErrorLines(string(out)), err
}

// caddyErrorLines drops Caddy's JSON info logs and keeps the error text.
func caddyErrorLines(out string) string {
	var keep []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, `"level":"info"`) || strings.Contains(line, `"level":"warn"`) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func (a *Agent) caddyRunning() bool {
	if a.systemd {
		return a.unitActive("fastcp-caddy")
	}
	return socketReady(CaddyAdminSocket)
}

// syncCaddy validates and activates a new Caddyfile. A failed reload
// restores the previous file and leaves the running server untouched.
func (a *Agent) syncCaddy(ctx context.Context, content string) error {
	old, _ := os.ReadFile(CaddyfilePath)
	running := a.caddyRunning()
	if bytes.Equal(old, []byte(content)) && running && !a.caddyRestart {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(CaddyfilePath), 0o755); err != nil {
		return err
	}
	next := CaddyfilePath + ".next"
	if err := writeFileAtomic(next, []byte(content), 0o644); err != nil {
		return err
	}
	defer os.Remove(next)
	if out, err := caddyValidate(ctx, next); err != nil {
		return fmt.Errorf("the generated web server configuration is invalid, nothing was changed%s", tail(out, 1500))
	}
	if len(old) > 0 {
		_ = writeFileAtomic(CaddyfilePath+".prev", old, 0o644)
	}
	if err := os.Rename(next, CaddyfilePath); err != nil {
		return err
	}

	if !running || a.caddyRestart {
		if err := a.startCaddy(ctx); err != nil {
			if len(old) > 0 {
				_ = writeFileAtomic(CaddyfilePath, old, 0o644)
				_ = a.startCaddy(ctx)
			}
			return err
		}
		a.caddyRestart = false
		clearCaddyRestart()
		return nil
	}
	if out, err := run(ctx, 90*time.Second, CaddyBinary, "reload", "--config", CaddyfilePath, "--adapter", "caddyfile"); err != nil {
		if len(old) > 0 {
			_ = writeFileAtomic(CaddyfilePath, old, 0o644)
		}
		return fmt.Errorf("the web server rejected the new configuration and kept the previous one%s", tail(caddyErrorLines(out), 1500))
	}
	return nil
}

func (a *Agent) startCaddy(ctx context.Context) error {
	if a.systemd {
		_ = a.systemctl(ctx, "enable", "--quiet", "fastcp-caddy")
		if err := a.systemctl(ctx, "restart", "fastcp-caddy"); err != nil {
			return fmt.Errorf("starting the web server failed: %w", err)
		}
		return nil
	}
	// Without systemd, mirror fastcp-caddy.service: its runtime directory,
	// the caddy user, and only the capability to bind ports 80 and 443.
	runDir := filepath.Dir(CaddyAdminSocket)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	cmd := exec.Command(CaddyBinary, "run", "--config", CaddyfilePath, "--adapter", "caddyfile")
	cmd.Env = append([]string{"PATH=/usr/local/bin:/usr/bin:/bin"}, caddyEnv()...)
	cmd.Dir = "/"
	if uid, gid := caddyIDs(); uid > 0 {
		if err := os.Chown(runDir, uid, gid); err != nil {
			return err
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential:  &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}},
			AmbientCaps: []uintptr{capNetBindService},
			Setsid:      true,
		}
	}
	if f, err := os.OpenFile(filepath.Join(LogDir, "caddy.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if socketReady(CaddyAdminSocket) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the web server did not start")
}

// ---------------------------------------------------------------------------
// Resource limits, suspension and cron

func sliceDropIn(uid int) string {
	return fmt.Sprintf("/etc/systemd/system/user-%d.slice.d/50-fastcp-limits.conf", uid)
}

func (a *Agent) syncLimits(ctx context.Context, p *plan, prev *State) []string {
	if !a.systemd {
		return nil
	}
	var warnings []string
	reload := false
	for name, u := range p.Users {
		t := p.Tenants[name]
		path := sliceDropIn(t.UID)
		content := sliceLimits(name, t.UID, u.MemoryMB, u.CPUPercent)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		changed, err := writeIfChanged(path, []byte(content), 0o644)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("user %s: resource limits: %v", name, err))
		}
		reload = reload || changed
	}
	if reload {
		if err := a.systemctl(ctx, "daemon-reload"); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings
}

func prevUser(prev *State, name string) (*StateUser, bool) {
	for i := range prev.Users {
		if prev.Users[i].Username == name {
			return &prev.Users[i], true
		}
	}
	return nil, false
}

func cronEqual(a, b []CronJob) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// syncAccounts applies suspension (account expiry, crontab removal, session
// termination) and writes crontabs that changed since the previous state.
func (a *Agent) syncAccounts(ctx context.Context, p *plan, prev *State) []string {
	var warnings []string
	names := make([]string, 0, len(p.Users))
	for n := range p.Users {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		u := p.Users[name]
		old, known := prevUser(prev, name)
		suspendChanged := !known || old.Suspended != u.Suspended
		if suspendChanged {
			expire := ""
			if u.Suspended {
				expire = "1"
			}
			if _, err := run(ctx, 30*time.Second, "usermod", "--expiredate", expire, name); err != nil {
				warnings = append(warnings, fmt.Sprintf("user %s: %v", name, err))
			}
			if u.Suspended && known {
				if a.systemd {
					_, _ = run(ctx, 30*time.Second, "loginctl", "terminate-user", name)
				}
			}
		}
		if !suspendChanged && known && cronEqual(old.Cron, u.Cron) {
			continue
		}
		if u.Suspended {
			_, _ = run(ctx, 30*time.Second, "crontab", "-r", "-u", name)
			continue
		}
		tab, w := renderCrontab(u.Cron)
		for _, msg := range w {
			warnings = append(warnings, fmt.Sprintf("user %s: %s", name, msg))
		}
		if len(u.Cron) == 0 {
			_, _ = run(ctx, 30*time.Second, "crontab", "-r", "-u", name)
			continue
		}
		if _, err := runEnv(ctx, 30*time.Second, nil, []byte(tab), "crontab", "-u", name, "-"); err != nil {
			warnings = append(warnings, fmt.Sprintf("user %s: crontab: %v", name, err))
		}
	}
	return warnings
}
