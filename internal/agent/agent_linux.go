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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
	"github.com/rehmatworks/fastcp/internal/version"
)

// Agent is the privileged fastcp-agent.
type Agent struct {
	socketPath string
	rpc        *RPCServer
	systemd    bool
	dev        bool

	// reloadMu serializes every change to generated configuration.
	reloadMu sync.Mutex

	// stateMu guards state and bootstrapped, so reads never wait for a
	// running apply.
	stateMu sync.RWMutex
	state   *State
	// custom is the administrator's Caddy configuration as last applied.
	custom []customFile
	// bootstrapped records tenants whose home layout was checked since start.
	bootstrapped map[string]bool
	// caddyRestart forces a full restart on the next apply (set by the
	// migration when the Caddy unit changed user or admin endpoint).
	caddyRestart bool

	aptMu sync.Mutex
	jobs  *jobStore

	phpMu       sync.Mutex
	phpCache    *phpDiscovery
	phpCachedAt time.Time

	ctx context.Context
}

// Run starts the agent and blocks until ctx is cancelled.
func Run(ctx context.Context, socketPath string) error {
	a := &Agent{
		socketPath:   socketPath,
		rpc:          NewRPCServer(),
		systemd:      hasSystemd(),
		dev:          os.Getenv("FASTCP_DEV") == "1",
		bootstrapped: map[string]bool{},
		ctx:          ctx,
	}
	if err := a.Setup(ctx); err != nil {
		slog.Error("setup failed; continuing with the existing layout", "error", err)
	}
	a.jobs = newJobStore(JobsPath, updateJobContinues)
	a.syncUpdateResult()
	a.loadState()
	a.custom = loadCustom()
	a.registerHandlers()

	panelUID := -1
	if u, err := user.Lookup(PanelUser); err == nil {
		panelUID, _ = strconv.Atoi(u.Uid)
	}
	a.rpc.Authorize = func(uid uint32) bool {
		return uid == 0 || (panelUID >= 0 && uid == uint32(panelUID))
	}

	ln, err := listenSocket(socketPath, PanelUser)
	if err != nil {
		return err
	}
	slog.Info("fastcp-agent listening", "socket", socketPath, "version", version.Version)

	go a.applyCachedState()
	go a.phpScheduler(ctx)
	go a.upgradeComponents(ctx)
	go siteFileAccessLoop(ctx)
	return a.rpc.Serve(ctx, ln)
}

// listenSocket creates the agent socket as root:group 0660.
func listenSocket(socketPath, group string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(socketPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", socketPath)
		}
		os.Remove(socketPath)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	gid := 0
	if g, err := user.LookupGroup(group); err == nil {
		gid, _ = strconv.Atoi(g.Gid)
	}
	if err := os.Chown(socketPath, 0, gid); err != nil {
		ln.Close()
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func (a *Agent) handle(method string, h HandlerFunc) { a.rpc.Handle(method, h) }

func (a *Agent) registerHandlers() {
	a.handle("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]string{"version": version.Version}, nil
	})
	a.handle("config.apply", a.handleApply)
	a.handle("auth.verify", a.handleAuthVerify)
	a.handle("auth.changePassword", a.handleChangePassword)
	a.handle("user.create", a.handleCreateUser)
	a.handle("user.delete", a.handleDeleteUser)
	a.handle("user.exec", a.handleUserExec)
	a.handle("backup.prepareSpool", a.handlePrepareSpool)
	a.handle("site.create", a.handleCreateSite)
	a.handle("site.delete", a.handleDeleteSite)
	a.handle("site.installWordPress", a.handleInstallWordPress)
	a.handle("site.readLog", a.handleReadSiteLog)
	a.handle("database.create", a.handleCreateDatabase)
	a.handle("database.delete", a.handleDeleteDatabase)
	a.handle("database.resetPassword", a.handleResetDatabasePassword)
	a.handle("mysql.getConfig", a.handleGetMySQLConfig)
	a.handle("mysql.setConfig", a.handleSetMySQLConfig)
	a.handle("ssh.addKey", a.handleAddSSHKey)
	a.handle("ssh.removeKey", a.handleRemoveSSHKey)
	a.handle("ssh.getConfig", a.handleGetSSHConfig)
	a.handle("ssh.setConfig", a.handleSetSSHConfig)
	a.handle("system.status", a.handleSystemStatus)
	a.handle("system.services", a.handleServices)
	a.handle("caddy.getConfig", a.handleGetCaddyConfig)
	a.handle("caddy.setConfig", a.handleSetCaddyConfig)
	a.handle("caddy.customStatus", a.handleCustomStatus)
	a.handle("caddy.applyCustom", a.handleApplyCustom)
	a.handle("firewall.status", a.handleFirewallStatus)
	a.handle("firewall.install", a.handleFirewallInstall)
	a.handle("firewall.setEnabled", a.handleFirewallSetEnabled)
	a.handle("firewall.allow", a.handleFirewallAllow)
	a.handle("firewall.deny", a.handleFirewallDeny)
	a.handle("firewall.delete", a.handleFirewallDelete)
	a.handle("rclone.status", a.handleRcloneStatus)
	a.handle("rclone.install", a.handleRcloneInstall)
	a.handle("php.status", a.handlePHPStatus)
	a.handle("php.install", a.handlePHPInstall)
	a.handle("php.runMaintenance", a.handlePHPMaintenance)
	a.handle("php.setSettings", a.handlePHPSetSettings)
	a.handle("job.get", a.handleJobGet)
	a.handle("update.apply", a.handleUpdateApply)
	a.handle("update.status", a.handleUpdateStatus)
}

// ---------------------------------------------------------------------------
// Command helpers

// run executes a command with a timeout and returns its combined output.
func run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	return runEnv(ctx, timeout, nil, nil, name, args...)
}

func runEnv(ctx context.Context, timeout time.Duration, env []string, stdin []byte, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		return text, fmt.Errorf("%s: %w%s", name, err, tail(text, 2000))
	}
	return text, nil
}

func tail(s string, n int) string {
	if s == "" {
		return ""
	}
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return ": " + s
}

func hasSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func (a *Agent) systemctl(ctx context.Context, args ...string) error {
	if !a.systemd {
		return errors.New("systemd is not available")
	}
	_, err := run(ctx, 2*time.Minute, "systemctl", args...)
	return err
}

func (a *Agent) unitActive(unit string) bool {
	if !a.systemd {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
}

func (a *Agent) unitExists(unit string) bool {
	if !a.systemd {
		return false
	}
	out, err := exec.Command("systemctl", "show", "-p", "LoadState", "--value", unit).Output()
	return err == nil && strings.TrimSpace(string(out)) == "loaded"
}

// writeFileAtomic writes a root-owned file in a root-controlled directory.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".fastcp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeIfChanged writes path only when its content differs.
func writeIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != perm {
			return false, os.Chmod(path, perm)
		}
		return false, nil
	}
	return true, writeFileAtomic(path, data, perm)
}

func systemRAMMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 1024
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
			if kb, err := strconv.Atoi(f[1]); err == nil && kb > 0 {
				return kb / 1024
			}
		}
	}
	return 1024
}

// ---------------------------------------------------------------------------
// Tenants

func lookupIDs(name string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return -1, -1, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

// lookupTenant resolves a system account that may own sites: uid >= 1000,
// not reserved, with an absolute home directory.
func lookupTenant(name string) (*tenant, error) {
	if err := validate.TenantUsername(name); err != nil {
		return nil, err
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, notFoundf("system user %s does not exist", name)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if uid < 1000 || uid == 65534 {
		return nil, deniedf("%s is a system account", name)
	}
	home := filepath.Clean(u.HomeDir)
	if !filepath.IsAbs(home) || home == "/" {
		return nil, deniedf("%s has an invalid home directory", name)
	}
	return &tenant{Name: name, UID: uid, GID: gid, Home: home}, nil
}

// registeredTenant resolves name and requires it to be in the applied state.
func (a *Agent) registeredTenant(name string) (*tenant, error) {
	t, err := lookupTenant(name)
	if err != nil {
		return nil, err
	}
	for _, u := range a.currentState().Users {
		if u.Username == name {
			return t, nil
		}
	}
	return nil, deniedf("%s is not a FastCP user", name)
}

// currentState returns the last applied state (never nil). Callers must
// not modify it.
func (a *Agent) currentState() *State {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	if a.state == nil {
		return &State{}
	}
	return a.state
}

func (a *Agent) isBootstrapped(name string) bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.bootstrapped[name]
}

func (a *Agent) setBootstrapped(name string, ok bool) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if ok {
		a.bootstrapped[name] = true
	} else {
		delete(a.bootstrapped, name)
	}
}

func supplementaryGroups(name string) []uint32 {
	u, err := user.Lookup(name)
	if err != nil {
		return nil
	}
	ids, err := u.GroupIds()
	if err != nil {
		return nil
	}
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if n, err := strconv.Atoi(id); err == nil {
			out = append(out, uint32(n))
		}
	}
	return out
}

func caddyIDs() (int, int) {
	uid, gid, err := lookupIDs(CaddyUser)
	if err != nil {
		return -1, -1
	}
	return uid, gid
}

func groupID(name string) int {
	g, err := user.LookupGroup(name)
	if err != nil {
		return -1
	}
	gid, _ := strconv.Atoi(g.Gid)
	return gid
}
