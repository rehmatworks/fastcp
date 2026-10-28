//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

// pamSlots bounds concurrent PAM conversations so password guessing cannot
// exhaust the agent.
var pamSlots = make(chan struct{}, 4)

func (a *Agent) handleAuthVerify(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[AuthRequest](params)
	if err != nil {
		return nil, err
	}
	fail := deniedf("invalid username or password")
	if validate.Username(req.Username) != nil || req.Password == "" || len(req.Password) > 1024 {
		return nil, fail
	}
	uid, _, err := lookupIDs(req.Username)
	if err != nil || uid < 1000 || uid == 65534 {
		return nil, fail
	}
	select {
	case pamSlots <- struct{}{}:
		defer func() { <-pamSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := pamAuthenticate(req.Username, req.Password); err != nil {
		slog.Info("login rejected", "user", req.Username, "reason", err)
		return nil, fail
	}
	return map[string]bool{"ok": true}, nil
}

func setPassword(ctx context.Context, username, password string) error {
	if err := validate.Password(password); err != nil {
		return invalid(err)
	}
	if strings.ContainsAny(password, "\r\n:") {
		return invalidf("password cannot contain line breaks or ':'")
	}
	if _, err := runEnv(ctx, 30*time.Second, nil, []byte(username+":"+password+"\n"), "chpasswd"); err != nil {
		return errors.New("setting the password failed")
	}
	return nil
}

func (a *Agent) handleChangePassword(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[AuthRequest](params)
	if err != nil {
		return nil, err
	}
	if _, err := lookupTenant(req.Username); err != nil {
		return nil, err
	}
	if err := setPassword(ctx, req.Username, req.Password); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}

func (a *Agent) handleCreateUser(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[CreateUserRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.NewUsername(req.Username); err != nil {
		return nil, invalid(err)
	}
	if err := validate.Password(req.Password); err != nil {
		return nil, invalid(err)
	}
	if _, err := user.Lookup(req.Username); err == nil {
		return nil, conflictf("a system account named %s already exists", req.Username)
	}
	if _, err := user.LookupGroup(req.Username); err == nil {
		return nil, conflictf("a system group named %s already exists", req.Username)
	}
	if out, err := run(ctx, time.Minute, "useradd", "--create-home", "--user-group", "--shell", "/bin/bash", "--", req.Username); err != nil {
		return nil, fmt.Errorf("creating the account failed%s", tail(out, 500))
	}
	cleanup := func() { _, _ = run(context.WithoutCancel(ctx), time.Minute, "userdel", "--remove", req.Username) }
	t, err := lookupTenant(req.Username)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.Chmod(t.Home, 0o750); err != nil {
		cleanup()
		return nil, err
	}
	if err := setPassword(ctx, req.Username, req.Password); err != nil {
		cleanup()
		return nil, err
	}
	if err := bootstrapTenant(t); err != nil {
		cleanup()
		return nil, err
	}
	a.setBootstrapped(t.Name, true)
	slog.Info("created user", "user", t.Name, "uid", t.UID)
	return map[string]string{"status": "ok"}, nil
}

func (a *Agent) handleDeleteUser(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[DeleteUserRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := lookupTenant(req.Username)
	if err != nil {
		return nil, err
	}
	for _, d := range req.Databases {
		if err := validateDBRequest(d.DBName, d.DBUser, ""); err != nil {
			return nil, err
		}
	}

	cur := a.currentState()
	st := State{}
	for _, u := range cur.Users {
		if u.Username != t.Name {
			st.Users = append(st.Users, u)
		}
	}
	for _, s := range cur.Sites {
		if s.Username != t.Name {
			st.Sites = append(st.Sites, s)
		}
	}
	if _, err := a.apply(ctx, &st, true); err != nil {
		return nil, fmt.Errorf("removing the user's sites from the web server failed: %w", err)
	}

	_, _ = run(ctx, 30*time.Second, "crontab", "-r", "-u", t.Name)
	if a.systemd {
		_, _ = run(ctx, 30*time.Second, "loginctl", "terminate-user", t.Name)
	}
	_, _ = run(ctx, 30*time.Second, "pkill", "-KILL", "-u", t.Name)

	var warnings []string
	for _, d := range req.Databases {
		if err := dropDatabase(ctx, d.DBName, d.DBUser); err != nil {
			warnings = append(warnings, fmt.Sprintf("database %s: %v", d.DBName, err))
		}
	}

	if err := os.RemoveAll(filepath.Dir(sliceDropIn(t.UID))); err == nil && a.systemd {
		_ = a.systemctl(ctx, "daemon-reload")
	}
	for _, dir := range []string{
		filepath.Join(PHPSocketRoot, t.Name),
		filepath.Join(SiteLogRoot, t.Name),
		filepath.Join(ExportSpoolDir, t.Name),
		filepath.Join(LegacyUserConfigDir, t.Name),
	} {
		_ = os.RemoveAll(dir)
	}

	var out string
	for attempt := 0; attempt < 3; attempt++ {
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		if out, err = run(ctx, 5*time.Minute, "userdel", "--remove", "--", t.Name); err == nil {
			break
		}
		_, _ = run(ctx, 30*time.Second, "pkill", "-KILL", "-u", t.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("removing the account failed%s", tail(out, 500))
	}
	a.setBootstrapped(t.Name, false)
	slog.Info("deleted user", "user", t.Name)
	return map[string]any{"status": "ok", "warnings": warnings}, nil
}
