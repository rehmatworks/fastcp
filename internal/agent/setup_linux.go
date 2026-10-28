//go:build linux

package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
)

const (
	// layoutMarker records that the FastCP 3 layout migration completed.
	layoutMarker = MigrationsDir + "/layout-3"
	// caddyRestartFlag survives agent restarts until the web server has been
	// restarted under its new unit.
	caddyRestartFlag = MigrationsDir + "/caddy-restart"
	pmaConfigPath    = PMADir + "/config.inc.php"
)

// RunSetup performs Setup once and exits; used by the installer and updates.
func RunSetup(ctx context.Context) error {
	a := &Agent{systemd: hasSystemd(), dev: os.Getenv("FASTCP_DEV") == "1", bootstrapped: map[string]bool{}, ctx: ctx}
	return a.Setup(ctx)
}

// Setup creates service accounts, directories, units and system files, and
// migrates a FastCP 2 installation. It is idempotent and runs on every start.
func (a *Agent) Setup(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("fastcp-agent must run as root")
	}
	if err := ensureServiceUsers(ctx); err != nil {
		return err
	}
	if err := ensureLayout(); err != nil {
		return err
	}
	if _, err := writeIfChanged("/etc/pam.d/fastcp", []byte(pamConfig), 0o644); err != nil {
		slog.Warn("could not write the PAM configuration", "error", err)
	}

	migrating := !fileExists(layoutMarker)
	if migrating && a.systemd {
		_ = a.systemctl(ctx, "stop", "fastcp.service")
	}
	if err := fixPanelOwnership(); err != nil {
		slog.Warn("could not fix panel data ownership", "error", err)
	}
	unitsChanged, err := a.writeUnits(ctx)
	if err != nil {
		return err
	}

	if migrating {
		slog.Info("migrating to the FastCP 3 layout")
		migrateCaddyStorage()
		migrateTenantHomes()
		cleanLegacyFiles(a)
		if err := writeFileAtomic(caddyRestartFlag, nil, 0o600); err != nil {
			return err
		}
		if err := writeFileAtomic(layoutMarker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
			return err
		}
		slog.Info("FastCP 3 layout migration finished")
	}
	a.caddyRestart = fileExists(caddyRestartFlag)

	ensurePMAConfig()
	ensureMOTD()
	for v := range installedPHPBranches() {
		writeFPMIni(v)
	}
	a.ensureMySQLTuning(ctx)

	if a.systemd {
		if migrating || unitsChanged {
			_ = a.systemctl(ctx, "enable", "--quiet", "fastcp-agent.service", "fastcp.service", "fastcp-caddy.service")
		}
		if migrating {
			// The FastCP 3 panel refuses to run as root, so under the old
			// unit it may have hit systemd's start limit.
			_ = a.systemctl(ctx, "reset-failed", "fastcp.service")
			_ = a.systemctl(ctx, "restart", "fastcp.service")
		} else if unitsChanged && a.unitActive("fastcp.service") {
			_ = a.systemctl(ctx, "restart", "fastcp.service")
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ensureServiceUsers creates the unprivileged accounts the panel and the
// web server run as.
func ensureServiceUsers(ctx context.Context) error {
	for _, acct := range []struct{ name, home string }{
		{PanelUser, "/nonexistent"},
		{CaddyUser, CaddyStorageDir},
	} {
		if _, err := user.Lookup(acct.name); err == nil {
			continue
		}
		args := []string{"--system", "--user-group", "--no-create-home", "--home-dir", acct.home, "--shell", "/usr/sbin/nologin", acct.name}
		if _, err := run(ctx, time.Minute, "useradd", args...); err != nil {
			return fmt.Errorf("creating the %s account: %w", acct.name, err)
		}
	}
	return nil
}

// ensureLayout creates FastCP's directories with their owners and modes.
func ensureLayout() error {
	panelUID, panelGID, err := lookupIDs(PanelUser)
	if err != nil {
		return err
	}
	caddyUID, caddyGID := caddyIDs()
	pmaUID, pmaGID, _ := lookupIDs(PMAUser)
	dirs := []struct {
		path     string
		perm     os.FileMode
		uid, gid int
	}{
		{BaseDir, 0o755, 0, 0},
		{BinDir, 0o755, 0, 0},
		{ConfigDir, 0o755, 0, 0},
		{MigrationsDir, 0o700, 0, 0},
		{DataDir, 0o700, panelUID, panelGID},
		{SSLDir, 0o700, panelUID, panelGID},
		{RunDir, 0o755, 0, 0},
		{PHPSocketRoot, 0o755, 0, 0},
		{PrivateTmpDir, 0o700, 0, 0},
		{SpoolRoot, 0o755, 0, 0},
		{ExportSpoolDir, 0o755, 0, 0},
		{WWWDir, 0o755, 0, 0},
		{SuspendedDir, 0o755, 0, 0},
		{"/var/lib/fastcp", 0o755, 0, 0},
		{CaddyStorageDir, 0o700, caddyUID, caddyGID},
		{LogDir, 0o755, 0, 0},
		{SiteLogRoot, 0o755, 0, 0},
	}
	if pmaUID >= 0 {
		dirs = append(dirs, struct {
			path     string
			perm     os.FileMode
			uid, gid int
		}{PMATmpDir, 0o700, pmaUID, pmaGID})
	}
	for _, d := range dirs {
		if d.uid < 0 {
			d.uid, d.gid = 0, 0
		}
		if err := ensureOwnedDir(d.path, d.perm, d.uid, d.gid); err != nil {
			return fmt.Errorf("preparing %s: %w", d.path, err)
		}
	}
	if _, err := writeIfChanged(filepath.Join(SuspendedDir, "index.html"), []byte(suspendedPage), 0o644); err != nil {
		return err
	}
	if pmaUID >= 0 {
		ensureLogFile(filepath.Join(LogDir, "phpmyadmin-error.log"), pmaUID, pmaGID)
	}
	return nil
}

// ensureLogFile creates a log file owned by uid, replacing anything that is
// not a regular file (FastCP 2 let users create entries in the log directory).
func ensureLogFile(path string, uid, gid int) {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		slog.Warn("could not create log file", "path", path, "error", err)
		return
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
			slog.Warn("refusing to use hard-linked log file", "path", path)
			return
		}
	}
	_ = f.Chown(uid, gid)
	_ = f.Chmod(0o640)
}

// fixPanelOwnership hands the panel's data and certificate to fastcp-panel.
// Symlinks are never followed.
func fixPanelOwnership() error {
	uid, gid, err := lookupIDs(PanelUser)
	if err != nil {
		return err
	}
	var errs []error
	for _, root := range []string{DataDir, SSLDir} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return os.Remove(path)
			}
			if err := os.Lchown(path, uid, gid); err != nil {
				return err
			}
			switch {
			case d.IsDir():
				return os.Chmod(path, 0o700)
			case strings.HasSuffix(path, ".crt"):
				return os.Chmod(path, 0o644)
			default:
				return os.Chmod(path, 0o600)
			}
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// writeUnits installs FastCP's systemd units and reports whether any changed.
func (a *Agent) writeUnits(ctx context.Context) (bool, error) {
	if !a.systemd {
		return false, nil
	}
	changed := false
	names := make([]string, 0, 3)
	for name := range units() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c, err := writeIfChanged(filepath.Join(unitDir, name), []byte(units()[name]), 0o644)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	legacy, _ := filepath.Glob(filepath.Join(unitDir, "fastcp-php@*.service"))
	for _, l := range legacy {
		_ = a.systemctl(ctx, "disable", "--now", filepath.Base(l))
		_ = os.Remove(l)
		changed = true
	}
	if changed {
		if err := a.systemctl(ctx, "daemon-reload"); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// migrateCaddyStorage moves certificates issued by the FastCP 2 web server,
// which ran as root, to the caddy account's storage.
func migrateCaddyStorage() {
	uid, gid := caddyIDs()
	if uid < 0 {
		return
	}
	entries, _ := os.ReadDir(CaddyStorageDir)
	if len(entries) == 0 {
		for _, src := range []string{"/root/.local/share/caddy", "/.local/share/caddy"} {
			if fi, err := os.Lstat(src); err != nil || !fi.IsDir() {
				continue
			}
			if out, err := exec.Command("cp", "-a", "--no-dereference", src+"/.", CaddyStorageDir+"/").CombinedOutput(); err != nil {
				slog.Warn("copying web server certificates failed", "from", src, "error", err, "output", string(out))
				continue
			}
			slog.Info("copied web server certificates", "from", src)
			break
		}
	}
	_ = filepath.WalkDir(CaddyStorageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		return os.Lchown(path, uid, gid)
	})
}

// migrateTenantHomes gives the web server access to existing sites through
// ACLs instead of world permissions, and hides wp-config.php files in
// public directories from it.
func migrateTenantHomes() {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < 1000 || uid == 65534 {
			continue
		}
		t, err := lookupTenant(f[0])
		if err != nil {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(t.Home, "apps")); err != nil || !fi.IsDir() {
			continue
		}
		if err := bootstrapTenant(t); err != nil {
			slog.Warn("could not prepare home directory", "user", t.Name, "error", err)
			continue
		}
		home, err := openHome(t)
		if err != nil {
			continue
		}
		if info, err := home.Stat(); err == nil && info.Perm()&0o007 != 0 {
			_ = home.Chmod(info.Perm() &^ 0o007)
		}
		if apps, err := home.OpenDir("apps"); err == nil {
			names, _ := apps.Names()
			for _, name := range names {
				site, err := apps.OpenDir(name)
				if err != nil {
					continue
				}
				grantSiteAccess(t, site, true)
				site.Close()
			}
			apps.Close()
		}
		// FastCP 2 kept FPM sockets in ~/.fastcp/run.
		if legacy, err := home.OpenDir(".fastcp"); err == nil {
			legacy.Close()
			_ = home.RemoveAll(".fastcp")
		}
		home.Close()
	}
}

// cleanLegacyFiles removes FastCP 2 leftovers that are unsafe or unused.
func cleanLegacyFiles(a *Agent) {
	entries, _ := os.ReadDir(LogDir)
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 {
			_ = os.Remove(filepath.Join(LogDir, e.Name()))
		}
	}
	for _, p := range []string{"/opt/fastcp/run/phpmyadmin.sock", "/etc/tmpfiles.d/fastcp.conf", "/opt/fastcp/config/php/99-fastcp.ini"} {
		_ = os.Remove(p)
	}
	_ = os.Remove("/opt/fastcp/config/php")
}

var blowfishRE = regexp.MustCompile(`\$cfg\['blowfish_secret'\]\s*=\s*'([A-Za-z0-9]{32,64})';`)

// ensurePMAConfig writes phpMyAdmin's configuration. Credentials come from
// HTTP basic auth added by the panel's proxy for one request at a time.
func ensurePMAConfig() {
	if _, err := os.Stat(filepath.Join(PMADir, "index.php")); err != nil {
		return
	}
	secret := ""
	if data, err := os.ReadFile(pmaConfigPath); err == nil {
		if m := blowfishRE.FindSubmatch(data); m != nil {
			secret = string(m[1])
		}
	}
	if secret == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		secret = hex.EncodeToString(b)
	}
	content := fmt.Sprintf(`<?php
// Managed by FastCP.
$cfg['blowfish_secret'] = '%s';
$cfg['TempDir'] = '%s';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';

$cfg['Servers'][1]['host'] = '127.0.0.1';
$cfg['Servers'][1]['auth_type'] = 'config';
$cfg['Servers'][1]['user'] = $_SERVER['PHP_AUTH_USER'] ?? '';
$cfg['Servers'][1]['password'] = $_SERVER['PHP_AUTH_PW'] ?? '';
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['Servers'][1]['AllowRoot'] = false;
$cfg['Servers'][1]['hide_db'] = '^(information_schema|performance_schema|mysql|sys)$';

$cfg['ShowCreateDb'] = false;
$cfg['LoginCookieValidity'] = 3600;
$cfg['LoginCookieStore'] = 0;
$cfg['LoginCookieDeleteAll'] = true;
`, secret, PMATmpDir)
	_, gid, err := lookupIDs(PMAUser)
	if err != nil {
		gid = 0
	}
	if _, err := writeIfChanged(pmaConfigPath, []byte(content), 0o640); err != nil {
		slog.Warn("could not write the phpMyAdmin configuration", "error", err)
		return
	}
	_ = os.Lchown(pmaConfigPath, 0, gid)
	for _, p := range []string{"signon.php", ".user.ini"} {
		_ = os.Remove(filepath.Join(PMADir, p))
	}
}

// clearCaddyRestart is called after the web server restarted under the
// FastCP 3 unit.
func clearCaddyRestart() {
	_ = os.Remove(caddyRestartFlag)
}
