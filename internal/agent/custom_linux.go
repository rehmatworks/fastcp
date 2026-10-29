//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rehmatworks/fastcp/internal/validate"
)

var customIndexPath = filepath.Join(CaddyAppliedDir, "index.json")

// loadCustom reads the applied custom configuration. Entries whose copy is
// missing are dropped, since importing them would stop Caddy from loading.
func loadCustom() []customFile {
	data, err := os.ReadFile(customIndexPath)
	if err != nil {
		return nil
	}
	var list []customFile
	if err := json.Unmarshal(data, &list); err != nil {
		slog.Warn("ignoring unreadable custom configuration index", "path", customIndexPath, "error", err)
		return nil
	}
	var out []customFile
	for _, f := range list {
		if !f.valid() {
			continue
		}
		if fi, err := os.Lstat(f.copyPath()); err != nil || !fi.Mode().IsRegular() {
			slog.Warn("custom configuration copy is missing; apply it again", "file", filepath.Join(CaddyCustomDir, f.Rel))
			continue
		}
		out = append(out, f)
	}
	return out
}

func (a *Agent) appliedCustom() []customFile {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.custom
}

func (a *Agent) swapCustom(next []customFile) []customFile {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	prev := a.custom
	a.custom = next
	return prev
}

// siteKeys returns "username/slug" for every site in the pushed state.
func (a *Agent) siteKeys() map[string]bool {
	out := map[string]bool{}
	if st := a.currentState(); st != nil {
		for _, s := range st.Sites {
			if validate.TenantUsername(s.Username) == nil && validate.Slug(s.Slug) == nil {
				out[s.Username+"/"+s.Slug] = true
			}
		}
	}
	return out
}

func (a *Agent) handleCustomStatus(ctx context.Context, _ json.RawMessage) (any, error) {
	return customStatus(CaddyCustomDir, scanCustom(CaddyCustomDir, a.siteKeys(), rootOnly), a.appliedCustom()), nil
}

// handleApplyCustom loads the administrator's current files. Caddy checks
// the whole configuration first; if it fails, the previous files stay.
func (a *Agent) handleApplyCustom(ctx context.Context, _ json.RawMessage) (any, error) {
	if a.currentState() == nil {
		return nil, errors.New("the control panel has not configured the web server yet; try again in a minute")
	}
	found := scanCustom(CaddyCustomDir, a.siteKeys(), rootOnly)
	if err := ensureAppliedDir(); err != nil {
		return nil, err
	}
	var next []customFile
	var loaded []customSource
	for _, s := range found {
		if s.Problem != "" {
			continue
		}
		dst := s.copyPath()
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if _, err := writeIfChanged(dst, s.Data, 0o644); err != nil {
			return nil, err
		}
		next = append(next, s.customFile)
		loaded = append(loaded, s)
	}

	prev := a.swapCustom(next)
	if err := a.reapply(ctx); err != nil {
		a.swapCustom(prev)
		pruneCustomCopies(prev)
		return nil, errors.New(applyError(err.Error(), loaded))
	}
	data, _ := json.MarshalIndent(next, "", "  ")
	if err := writeFileAtomic(customIndexPath, data, 0o644); err != nil {
		slog.Warn("could not save the custom configuration index", "error", err)
	}
	pruneCustomCopies(next)
	return customStatus(CaddyCustomDir, found, next), nil
}

// ensureAppliedDir keeps the copies readable by root and Caddy only, since
// custom configuration can hold credentials.
func ensureAppliedDir() error {
	_, gid := caddyIDs()
	if gid < 0 {
		gid = 0
	}
	return ensureOwnedDir(CaddyAppliedDir, 0o750, 0, gid)
}

// pruneCustomCopies removes copies that keep is not using.
func pruneCustomCopies(keep []customFile) {
	used := map[string]bool{}
	for _, f := range keep {
		used[f.Hash] = true
	}
	entries, _ := os.ReadDir(CaddyAppliedDir)
	for _, e := range entries {
		if e.IsDir() && customHashRE.MatchString(e.Name()) && !used[e.Name()] {
			_ = os.RemoveAll(filepath.Join(CaddyAppliedDir, e.Name()))
		}
	}
}

// ensureCustomDir creates the folder for the administrator's configuration.
// Only root reads it; Caddy loads the applied copies.
func ensureCustomDir() error {
	for _, d := range []string{CaddyCustomDir, filepath.Join(CaddyCustomDir, "sites")} {
		if err := ensureOwnedDir(d, 0o700, 0, 0); err != nil {
			return err
		}
	}
	if _, err := writeIfChanged(filepath.Join(CaddyCustomDir, "README"), []byte(customReadme), 0o644); err != nil {
		return err
	}
	return ensureAppliedDir()
}
