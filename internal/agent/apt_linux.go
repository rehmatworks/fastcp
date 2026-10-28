//go:build linux

package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// apt runs apt-get non-interactively, serialized with every other package
// operation of the agent, waiting for locks held by other tools.
func (a *Agent) apt(ctx context.Context, log io.Writer, timeout time.Duration, args ...string) error {
	a.aptMu.Lock()
	defer a.aptMu.Unlock()
	return aptLocked(ctx, log, timeout, args...)
}

func aptLocked(ctx context.Context, log io.Writer, timeout time.Duration, args ...string) error {
	full := append([]string{
		"-q", "-y",
		"-o", "DPkg::Lock::Timeout=600",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}, args...)
	env := []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a", "APT_LISTCHANGES_FRONTEND=none"}
	return streamCmdEnv(ctx, log, timeout, env, "apt-get", full...)
}

// streamCmd runs a command, copying its output to log.
func streamCmd(ctx context.Context, log io.Writer, timeout time.Duration, name string, args ...string) error {
	return streamCmdEnv(ctx, log, timeout, nil, name, args...)
}

func streamCmdEnv(ctx context.Context, log io.Writer, timeout time.Duration, env []string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if log != nil {
		fmt.Fprintf(log, "$ %s %s\n", name, strings.Join(args, " "))
		cmd.Stdout, cmd.Stderr = log, log
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s timed out after %s", name, timeout)
		}
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}
