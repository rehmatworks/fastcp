//go:build linux

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxExecTimeout   = 6 * time.Hour
	defaultExecOut   = 1 << 20
	maxExecOutput    = 16 << 20
	tenantSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

var envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// envDenied are variables that change how bash or the dynamic loader behave.
func envDenied(k string) bool {
	if strings.HasPrefix(k, "LD_") || strings.HasPrefix(k, "BASH_") {
		return true
	}
	switch k {
	case "ENV", "SHELLOPTS", "BASHOPTS", "PS4", "IFS", "PATH", "HOME", "USER", "LOGNAME", "SHELL", "GCONV_PATH":
		return true
	}
	return false
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.truncated = true
	}
	return len(p), nil
}

type execOptions struct {
	Script    string
	Args      []string // when set, run Args directly instead of bash -c Script
	Env       map[string]string
	Stdin     io.Reader
	Dir       string
	Timeout   time.Duration
	MaxOutput int
}

// execAs runs a command as tenant t with only the tenant's groups and a
// minimal environment. The working directory is entered after dropping
// privileges, so a tenant-controlled path cannot redirect root.
func execAs(ctx context.Context, t *tenant, o execOptions) (*UserExecResult, error) {
	if o.Timeout <= 0 || o.Timeout > maxExecTimeout {
		o.Timeout = maxExecTimeout
	}
	if o.MaxOutput <= 0 {
		o.MaxOutput = defaultExecOut
	}
	if o.MaxOutput > maxExecOutput {
		o.MaxOutput = maxExecOutput
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	var cmd *exec.Cmd
	if len(o.Args) > 0 {
		cmd = exec.CommandContext(ctx, o.Args[0], o.Args[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-c", "set -euo pipefail\n"+o.Script)
	}
	env := []string{
		"HOME=" + t.Home,
		"USER=" + t.Name,
		"LOGNAME=" + t.Name,
		"SHELL=/bin/bash",
		"PATH=" + tenantSearchPath,
		"LANG=C.UTF-8",
		"TMPDIR=" + t.Home + "/.tmp",
	}
	for k, v := range o.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Dir = t.Home
	if o.Dir != "" {
		cmd.Dir = o.Dir
	}
	groups := supplementaryGroups(t.Name)
	if groups == nil {
		groups = []uint32{uint32(t.GID)}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(t.UID), Gid: uint32(t.GID), Groups: groups},
		Setsid:     true,
		Pdeathsig:  syscall.SIGKILL,
	}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	out := &tailBuffer{limit: o.MaxOutput}
	cmd.Stdout, cmd.Stderr = out, out
	if o.Stdin != nil {
		cmd.Stdin = o.Stdin
	}

	err := cmd.Run()
	res := &UserExecResult{Output: string(out.buf), Truncated: out.truncated}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() == context.DeadlineExceeded:
		res.ExitCode = -1
		res.Output += "\n[timed out]"
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return nil, err
	}
	return res, nil
}

func (a *Agent) handleUserExec(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[UserExecRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Script) == "" || len(req.Script) > 256<<10 {
		return nil, invalidf("script must be between 1 byte and 256 KiB")
	}
	for k := range req.Env {
		if !envKeyRE.MatchString(k) || envDenied(k) {
			return nil, invalidf("environment variable %q is not allowed", k)
		}
	}
	o := execOptions{
		Script:    req.Script,
		Env:       req.Env,
		Timeout:   time.Duration(req.TimeoutSec) * time.Second,
		MaxOutput: req.MaxOutput,
	}
	if req.Stdin != "" {
		o.Stdin = bytes.NewReader([]byte(req.Stdin))
	}
	return execAs(ctx, t, o)
}

func (a *Agent) handlePrepareSpool(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[SpoolRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	dir, err := ensureSpool(t)
	if err != nil {
		return nil, err
	}
	return &SpoolResult{Dir: dir}, nil
}
