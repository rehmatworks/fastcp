//go:build linux

package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/release"
	"github.com/rehmatworks/fastcp/internal/validate"
	"github.com/rehmatworks/fastcp/internal/version"
)

const (
	updatePendingPath = ConfigDir + "/update-pending.json"
	updateResultPath  = ConfigDir + "/update-result.json"
	updateTimeout     = 20 * time.Minute
)

type updatePending struct {
	JobID   string `json:"job_id"`
	Version string `json:"version"`
	Started string `json:"started"`
}

type updateResult struct {
	JobID   string `json:"job_id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Log     string `json:"log"`
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, v) == nil
}

// updateJobContinues keeps an update job running across the agent restart
// that the update itself causes.
func updateJobContinues(j *Job) bool {
	var p updatePending
	return j.Kind == "update" && readJSON(updatePendingPath, &p) && p.JobID == j.ID
}

// syncUpdateResult records the outcome reported by the update process.
func (a *Agent) syncUpdateResult() {
	var res updateResult
	if readJSON(updateResultPath, &res) {
		a.jobs.Finish(res.JobID, res.OK, res.Message, res.Log)
		_ = os.Remove(updateResultPath)
		_ = os.Remove(updatePendingPath)
		return
	}
	var p updatePending
	if !readJSON(updatePendingPath, &p) {
		return
	}
	if started, err := time.Parse(time.RFC3339, p.Started); err == nil && time.Since(started) > updateTimeout {
		a.jobs.Finish(p.JobID, false, "The update did not report back. Check `journalctl -u fastcp-update*`.", "")
		_ = os.Remove(updatePendingPath)
	}
}

func (a *Agent) handleUpdateStatus(ctx context.Context, params json.RawMessage) (any, error) {
	a.syncUpdateResult()
	return &UpdateStatus{CurrentVersion: version.Version, Job: a.jobs.Latest("update", "")}, nil
}

func (a *Agent) handleUpdateApply(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[UpdateRequest](params)
	if err != nil {
		return nil, err
	}
	target, err := validate.ParseSemVer(req.Version)
	if err != nil {
		return nil, invalid(err)
	}
	if cur, err := validate.ParseSemVer(version.Version); err == nil && target.Compare(cur) <= 0 {
		return nil, conflictf("FastCP %s is already installed", version.Version)
	}
	if !a.systemd {
		return nil, invalidf("updating from the panel requires systemd")
	}
	if _, err := release.PublicKey(); err != nil {
		return nil, deniedf("%v", err)
	}
	tag := target.Tag()
	return a.jobs.Start(a.ctx, "update", tag, func(ctx context.Context, log *JobLog) (string, error) {
		dir := filepath.Join(PrivateTmpDir, "update-"+tag)
		_ = os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		log.Printf("Downloading FastCP %s for %s", tag, runtime.GOARCH)
		if err := release.NewClient().Fetch(ctx, tag, dir, release.AssetNames(runtime.GOARCH)); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("Could not download or verify FastCP %s, so nothing was changed: %w", tag, err)
		}
		log.Printf("Signature and checksums verified")
		newAgent := filepath.Join(dir, "fastcp-agent-linux-"+runtime.GOARCH)
		out, err := run(ctx, 30*time.Second, newAgent, "--version")
		if err != nil || !strings.Contains(out, strings.TrimPrefix(tag, "v")) {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("the downloaded agent does not report version %s", tag)
		}
		pending, _ := json.Marshal(updatePending{JobID: log.ID(), Version: tag, Started: time.Now().UTC().Format(time.RFC3339)})
		if err := writeFileAtomic(updatePendingPath, pending, 0o600); err != nil {
			return "", err
		}
		_ = os.Remove(updateResultPath)
		unit := "fastcp-update-" + strings.ReplaceAll(strings.TrimPrefix(tag, "v"), ".", "-")
		_, _ = run(ctx, 10*time.Second, "systemctl", "reset-failed", unit+".service")
		if _, err := run(ctx, 30*time.Second, "systemd-run", "--unit", unit, "--collect", "--quiet",
			"--property=TimeoutStartSec=15min", newAgent, "update-finish", "--job", log.ID(), "--version", tag, "--dir", dir); err != nil {
			_ = os.Remove(updatePendingPath)
			return "", fmt.Errorf("could not start the update: %w", err)
		}
		log.Printf("Installing FastCP %s; the panel will restart", tag)
		return "Installing FastCP " + tag, errDetached
	}), nil
}

// FinishUpdate runs from the new agent binary in a transient systemd unit.
// It installs the staged binaries, runs setup, restarts FastCP, verifies
// both services and rolls back to the previous binaries on failure.
func FinishUpdate(ctx context.Context, jobID, tag, dir string) error {
	var logBuf bytes.Buffer
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		slog.Info(msg)
		logBuf.WriteString(msg + "\n")
	}
	report := func(ok bool, msg string) error {
		data, _ := json.Marshal(updateResult{JobID: jobID, OK: ok, Message: msg, Log: logBuf.String()})
		return writeFileAtomic(updateResultPath, data, 0o600)
	}
	defer os.RemoveAll(dir)

	a := &Agent{systemd: hasSystemd(), bootstrapped: map[string]bool{}, ctx: ctx}
	if !a.systemd {
		return report(false, "systemd is required")
	}
	arch := runtime.GOARCH
	bins := []struct{ src, dst string }{
		{filepath.Join(dir, "fastcp-linux-"+arch), filepath.Join(BinDir, "fastcp")},
		{filepath.Join(dir, "fastcp-agent-linux-"+arch), filepath.Join(BinDir, "fastcp-agent")},
	}
	for _, b := range bins {
		if err := copyFile(b.src, b.dst+".new", 0o755); err != nil {
			return report(false, "staging failed: "+err.Error())
		}
	}

	logf("Stopping the panel")
	_ = a.systemctl(ctx, "stop", "fastcp.service")
	for _, b := range bins {
		if fileExists(b.dst) {
			_ = os.Remove(b.dst + ".prev")
			if err := os.Link(b.dst, b.dst+".prev"); err != nil {
				_ = copyFile(b.dst, b.dst+".prev", 0o755)
			}
		}
		if err := os.Rename(b.dst+".new", b.dst); err != nil {
			rollback(ctx, a, bins, logf)
			return report(false, "installing binaries failed: "+err.Error())
		}
	}
	logf("Installed FastCP %s binaries", tag)

	err := func() error {
		if out, err := run(ctx, 10*time.Minute, filepath.Join(BinDir, "fastcp-agent"), "setup"); err != nil {
			return fmt.Errorf("setup failed: %v%s", err, tail(out, 1000))
		}
		logf("Restarting the agent")
		if err := a.systemctl(ctx, "restart", "fastcp-agent.service"); err != nil {
			return err
		}
		if err := waitAgent(ctx, strings.TrimPrefix(tag, "v")); err != nil {
			return err
		}
		logf("Starting the panel")
		if err := a.systemctl(ctx, "restart", "fastcp.service"); err != nil {
			return err
		}
		return waitPanel(ctx)
	}()
	if err != nil {
		logf("Update failed: %v", err)
		rollback(ctx, a, bins, logf)
		return report(false, "The update failed and the previous version was restored: "+err.Error())
	}
	for _, b := range bins {
		_ = os.Remove(b.dst + ".prev")
	}
	logf("FastCP %s is running", tag)
	return report(true, "Updated to FastCP "+tag+".")
}

func rollback(ctx context.Context, a *Agent, bins []struct{ src, dst string }, logf func(string, ...any)) {
	logf("Restoring the previous version")
	_ = a.systemctl(ctx, "stop", "fastcp.service")
	for _, b := range bins {
		if fileExists(b.dst + ".prev") {
			_ = os.Rename(b.dst+".prev", b.dst)
		}
	}
	_, _ = run(ctx, 10*time.Minute, filepath.Join(BinDir, "fastcp-agent"), "setup")
	_ = a.systemctl(ctx, "restart", "fastcp-agent.service")
	_ = a.systemctl(ctx, "restart", "fastcp.service")
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func waitAgent(ctx context.Context, want string) error {
	c := NewClient(DefaultSocket)
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		v, err := c.Ping(ctx)
		if err == nil && v == want {
			return nil
		}
		if err == nil {
			last = fmt.Errorf("agent reports version %s", v)
		} else {
			last = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the agent did not come back: %v", last)
}

func waitPanel(ctx context.Context) error {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/api/health", PanelPort)
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = errors.New(resp.Status)
		}
		last = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the panel did not become healthy: %v", last)
}
