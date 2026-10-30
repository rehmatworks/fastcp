//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/rehmatworks/fastcp/internal/safefs"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	// phpLogMax is the size at which a site's PHP error log is trimmed to
	// its newest phpLogKeep bytes.
	phpLogMax  = 50 << 20
	phpLogKeep = 10 << 20
)

// trimPHPLogs trims the PHP error logs that outgrew phpLogMax, so a site
// stuck logging the same error cannot fill the disk.
func trimPHPLogs() {
	for _, t := range homeTenants() {
		home, err := openHome(t)
		if err != nil {
			continue
		}
		apps, err := home.OpenDir("apps")
		home.Close()
		if err != nil {
			continue
		}
		sites, _ := apps.Names()
		for _, site := range sites {
			if err := trimPHPLog(apps, site, t); err != nil {
				slog.Warn("could not trim PHP error log", "user", t.Name, "site", site, "error", err)
			}
		}
		apps.Close()
	}
}

// trimPHPLog replaces a site's PHP error log with its newest phpLogKeep
// bytes once it is larger than phpLogMax. Links and files of other owners
// are left alone. PHP opens the log for each message, so it continues in
// the new file.
func trimPHPLog(apps *safefs.Dir, site string, t *tenant) error {
	dir, err := apps.OpenDir(filepath.Join(site, "logs"))
	if err != nil {
		return nil
	}
	defer dir.Close()
	if info, err := dir.Lstat("php-error.log"); err != nil || !info.IsRegular() || info.Size <= phpLogMax {
		return nil
	}
	f, info, err := dir.OpenFile("php-error.log")
	if err != nil {
		return err
	}
	defer f.Close()
	if int(info.UID) != t.UID || info.Nlink != 1 {
		return nil
	}
	data, _, err := readTail(f, info.Size, phpLogKeep)
	if err != nil {
		return err
	}
	if err := dir.WriteFile("php-error.log", []byte(data), info.Perm(), t.UID, t.GID); err != nil {
		return err
	}
	slog.Info("trimmed PHP error log", "user", t.Name, "site", site, "bytes", info.Size)
	return nil
}

// handleReadSiteLog returns the end of a site's PHP error log or access log.
// The tenant controls the PHP log's directory and Caddy writes the access
// log, so neither file is trusted: links are not followed and the file must
// be a single-link regular file owned by whoever writes it.
func (a *Agent) handleReadSiteLog(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[SiteLogRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	if err := validate.Slug(req.Slug); err != nil {
		return nil, invalid(err)
	}

	var dir *safefs.Dir
	var name string
	var owner int
	switch req.Kind {
	case SiteLogPHP:
		home, err := openHome(t)
		if err != nil {
			return nil, err
		}
		defer home.Close()
		dir, err = home.OpenDir(filepath.Join("apps", req.Slug, "logs"))
		if errors.Is(err, fs.ErrNotExist) {
			return &SiteLogResult{}, nil
		}
		if err != nil {
			return nil, err
		}
		name, owner = "php-error.log", t.UID
	case SiteLogAccess:
		root, err := safefs.OpenRoot(filepath.Join(SiteLogRoot, t.Name))
		if errors.Is(err, fs.ErrNotExist) {
			return &SiteLogResult{}, nil
		}
		if err != nil {
			return nil, err
		}
		defer root.Close()
		dir, err = root.OpenDir(req.Slug)
		if errors.Is(err, fs.ErrNotExist) {
			return &SiteLogResult{}, nil
		}
		if err != nil {
			return nil, err
		}
		name = "access.log"
		owner, _ = caddyIDs()
	default:
		return nil, invalid(fmt.Errorf("unknown log %q", req.Kind))
	}
	defer dir.Close()

	f, info, err := dir.OpenFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return &SiteLogResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if owner < 0 || int(info.UID) != owner || info.Nlink != 1 {
		return nil, deniedf("%s is not a log file FastCP wrote", name)
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	data, truncated, err := readTail(f, info.Size, siteLogTail)
	if err != nil {
		return nil, err
	}
	return &SiteLogResult{
		Exists:    true,
		Size:      info.Size,
		ModTime:   st.ModTime().UTC(),
		Data:      data,
		Truncated: truncated,
	}, nil
}
