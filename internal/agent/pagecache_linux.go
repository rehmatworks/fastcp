//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"syscall"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

// handlePageCacheStatus asks FastCP's Caddy build what its
// fastcp_page_cache handler makes of a site. It runs as the caddy user and
// reads through the same file system as the site is served with, so it
// sees the files exactly as the web server does.
func (a *Agent) handlePageCacheStatus(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[PageCacheRequest](params)
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
	if _, caddy, _, _ := versions.get(ctx); caddy != CaddyBuild {
		return &PageCacheStatus{}, nil
	}
	uid, gid := caddyIDs()
	if uid <= 0 {
		return nil, fmt.Errorf("user %s is missing", CaddyUser)
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	root := path.Join(t.Home, "apps", req.Slug, "public")
	cmd := exec.CommandContext(ctx, CaddyBinary, "fastcp-page-cache", "--root", root, "--uid", strconv.Itoa(t.UID))
	cmd.Env = append([]string{"PATH=/usr/local/bin:/usr/bin:/bin"}, caddyEnv()...)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("the web server could not read the site's page cache setup: %w%s", err, tail(string(exit.Stderr), 1000))
		}
		return nil, err
	}
	var st PageCacheStatus
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, fmt.Errorf("the web server's page cache report is unreadable: %w", err)
	}
	st.Available = true
	return &st, nil
}
