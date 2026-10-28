//go:build linux

package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rehmatworks/fastcp/internal/safefs"
)

// openHome opens a tenant home directory and checks that the tenant owns it.
func openHome(t *tenant) (*safefs.Dir, error) {
	home, err := safefs.OpenRoot(t.Home)
	if err != nil {
		return nil, fmt.Errorf("open home of %s: %w", t.Name, err)
	}
	info, err := home.Stat()
	if err != nil {
		home.Close()
		return nil, err
	}
	if int(info.UID) != t.UID {
		home.Close()
		return nil, deniedf("home directory %s is not owned by %s", t.Home, t.Name)
	}
	return home, nil
}

// bootstrapTenant creates the directories FastCP expects in a home and the
// root-owned runtime directories outside it. Safe to repeat.
func bootstrapTenant(t *tenant) error {
	home, err := openHome(t)
	if err != nil {
		return err
	}
	defer home.Close()
	caddyUID, _ := caddyIDs()

	apps, err := home.Mkdir("apps", 0o750, t.UID, t.GID)
	if err != nil {
		return err
	}
	defer apps.Close()
	if info, err := apps.Stat(); err == nil && int(info.UID) != t.UID {
		return deniedf("%s/apps is not owned by %s", t.Home, t.Name)
	}

	tmp, err := home.EnsureDir(".tmp", 0o700, t.UID, t.GID)
	if err != nil {
		return err
	}
	for _, sub := range []string{"sessions", "uploads", "cache", "wsdl"} {
		d, err := tmp.EnsureDir(sub, 0o700, t.UID, t.GID)
		if err != nil {
			tmp.Close()
			return err
		}
		d.Close()
	}
	tmp.Close()

	if caddyUID >= 0 {
		if err := home.SetUserACL(caddyUID, safefs.ACLExec, -1); err != nil {
			slog.Warn("could not grant web server access to home", "user", t.Name, "error", err)
		}
		if err := apps.SetUserACL(caddyUID, safefs.ACLExec, -1); err != nil {
			slog.Warn("could not grant web server access to apps", "user", t.Name, "error", err)
		}
	}
	return ensureTenantRuntime(t)
}

// ensureTenantRuntime prepares root-controlled per-tenant directories.
func ensureTenantRuntime(t *tenant) error {
	_, caddyGID := caddyIDs()
	if caddyGID < 0 {
		caddyGID = 0
	}
	if err := ensureOwnedDir(filepath.Join(PHPSocketRoot, t.Name), 0o750, 0, caddyGID); err != nil {
		return err
	}
	return ensureOwnedDir(filepath.Join(SiteLogRoot, t.Name), 0o750, 0, t.GID)
}

// ensureOwnedDir creates or fixes a directory in a root-owned tree.
func ensureOwnedDir(path string, perm os.FileMode, uid, gid int) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.Mkdir(path, perm); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		fi, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", path)
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// ensureSiteLayout makes sure a site's directories exist with the right
// owner and web server access. With create, the site directory must not
// exist yet.
func ensureSiteLayout(t *tenant, slug string, create bool) (*safefs.Dir, error) {
	home, err := openHome(t)
	if err != nil {
		return nil, err
	}
	defer home.Close()
	apps, err := home.Mkdir("apps", 0o750, t.UID, t.GID)
	if err != nil {
		return nil, err
	}
	defer apps.Close()
	if create {
		if _, err := apps.Lstat(slug); err == nil {
			return nil, conflictf("the directory apps/%s already exists in %s's home", slug, t.Name)
		}
	}
	site, err := apps.Mkdir(slug, 0o750, t.UID, t.GID)
	if err != nil {
		return nil, err
	}
	info, err := site.Stat()
	if err != nil || int(info.UID) != t.UID {
		site.Close()
		return nil, deniedf("apps/%s is not owned by %s", slug, t.Name)
	}
	for _, sub := range []struct {
		name string
		perm uint32
	}{{"public", 0o750}, {"logs", 0o750}, {"tmp", 0o700}} {
		d, err := site.Mkdir(sub.name, sub.perm, t.UID, t.GID)
		if err != nil {
			site.Close()
			return nil, err
		}
		d.Close()
	}
	grantSiteAccess(t, site, false)
	if err := ensureSiteLogDir(t, slug); err != nil {
		slog.Warn("could not prepare site log directory", "user", t.Name, "site", slug, "error", err)
	}
	return site, nil
}

// grantSiteAccess gives the web server traverse access to the site
// directory and read access to public/. With recursive, existing files in
// public/ are granted as well (used once when migrating from v2). Existing
// wp-config.php files in public/ are explicitly denied.
func grantSiteAccess(t *tenant, site *safefs.Dir, recursive bool) {
	caddyUID, _ := caddyIDs()
	if caddyUID < 0 {
		return
	}
	rx := safefs.ACLRead | safefs.ACLExec
	if err := site.SetUserACL(caddyUID, safefs.ACLExec, -1); err != nil {
		slog.Warn("setfacl on site directory failed", "user", t.Name, "path", site.Path(), "error", err)
		return
	}
	pub, err := site.OpenDir("public")
	if err != nil {
		return
	}
	defer pub.Close()
	if err := pub.SetUserACL(caddyUID, rx, int(rx)); err != nil {
		slog.Warn("setfacl on public directory failed", "user", t.Name, "path", pub.Path(), "error", err)
		return
	}
	if recursive {
		err := pub.Walk(func(parent *safefs.Dir, name string, info safefs.Info) error {
			switch {
			case info.IsDir():
				return parent.SetUserACLAt(name, caddyUID, rx, int(rx))
			case info.IsRegular():
				return parent.SetUserACLAt(name, caddyUID, safefs.ACLRead, -1)
			}
			return nil
		})
		if err != nil {
			slog.Warn("recursive web server ACL failed", "user", t.Name, "path", pub.Path(), "error", err)
		}
	}
	if info, err := pub.Lstat("wp-config.php"); err == nil && info.IsRegular() {
		if err := pub.SetUserACLAt("wp-config.php", caddyUID, 0, -1); err != nil {
			slog.Warn("could not hide wp-config.php from the web server", "path", pub.Path(), "error", err)
		}
	}
}

// ensureSiteLogDir creates /var/log/fastcp/sites/<user>/<slug>, writable by
// Caddy and readable by the tenant's group.
func ensureSiteLogDir(t *tenant, slug string) error {
	if err := ensureOwnedDir(filepath.Join(SiteLogRoot, t.Name), 0o750, 0, t.GID); err != nil {
		return err
	}
	caddyUID, _ := caddyIDs()
	if caddyUID < 0 {
		caddyUID = 0
	}
	return ensureOwnedDir(filepath.Join(SiteLogRoot, t.Name, slug), 0o2750, caddyUID, t.GID)
}

// ensureSpool prepares the export spool directory of a tenant: owned by the
// tenant, group fastcp-panel, setgid so the panel can read finished files.
func ensureSpool(t *tenant) (string, error) {
	panelGID := groupID(PanelUser)
	if panelGID < 0 {
		panelGID = 0
	}
	if err := ensureOwnedDir(ExportSpoolDir, 0o755, 0, 0); err != nil {
		return "", err
	}
	dir := filepath.Join(ExportSpoolDir, t.Name)
	if err := ensureOwnedDir(dir, 0o2770, t.UID, panelGID); err != nil {
		return "", err
	}
	return dir, nil
}
