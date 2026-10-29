//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/rehmatworks/fastcp/internal/safefs"
	"github.com/rehmatworks/fastcp/internal/validate"
)

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
