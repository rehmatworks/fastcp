//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rehmatworks/fastcp/internal/release"
	"github.com/rehmatworks/fastcp/internal/version"
)

// Provision installs the pinned Caddy and phpMyAdmin and the newest PHP
// version available. The installer runs it after setup.
func Provision(ctx context.Context, log io.Writer) error {
	a := &Agent{systemd: hasSystemd(), bootstrapped: map[string]bool{}, ctx: ctx}
	if os.Geteuid() != 0 {
		return errors.New("fastcp-agent must run as root")
	}
	if err := a.ensureComponents(ctx, log); err != nil {
		return err
	}
	return a.provisionPHP(ctx, log)
}

// ensureComponents installs or upgrades Caddy and phpMyAdmin.
func (a *Agent) ensureComponents(ctx context.Context, log io.Writer) error {
	if err := os.MkdirAll(PrivateTmpDir, 0o700); err != nil {
		return err
	}
	// The running agent and the installer's provision step both install
	// components, staging into the same paths.
	unlock, err := lockFile(ctx, filepath.Join(PrivateTmpDir, "components.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	var errs []error
	if err := a.ensureCaddy(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("installing Caddy %s: %w", CaddyBuild, err))
	}
	if err := ensurePMA(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("installing phpMyAdmin %s: %w", PMAVersion, err))
	}
	return errors.Join(errs...)
}

// lockFile waits for an exclusive lock on path, shared with other processes.
// Calling the returned function releases it.
func lockFile(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// upgradeComponents runs ensureComponents in the background of a running
// agent, retrying failures with growing delays.
func (a *Agent) upgradeComponents(ctx context.Context) {
	for delay := 5 * time.Minute; ; delay = min(2*delay, 6*time.Hour) {
		attempt, cancel := context.WithTimeout(ctx, 15*time.Minute)
		err := a.ensureComponents(attempt, slogWriter{})
		cancel()
		if err == nil {
			return
		}
		slog.Warn("could not upgrade bundled components", "error", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// ensureCaddy installs FastCP's Caddy build: the one the installer staged,
// or the one published with the FastCP release the agent belongs to.
func (a *Agent) ensureCaddy(ctx context.Context, log io.Writer) error {
	installed := ""
	if _, err := os.Stat(CaddyBinary); err == nil {
		installed = caddyVersion(ctx)
		if installed == "" {
			installed = "unknown"
		}
	}
	if installed == CaddyBuild {
		_ = os.Remove(StagedCaddy)
		return nil
	}
	work, err := os.MkdirTemp(PrivateTmpDir, "caddy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	bin, err := obtainCaddy(ctx, log, work)
	if err != nil {
		return err
	}
	out, err := run(ctx, 10*time.Second, bin, "version")
	if got := parseCaddyVersion(out); err != nil || got != CaddyBuild {
		return fmt.Errorf("the new Caddy binary reports version %q, want %s", got, CaddyBuild)
	}

	a.reloadMu.Lock()
	err = os.MkdirAll(filepath.Dir(CaddyBinary), 0o755)
	if err == nil {
		err = copyFile(bin, CaddyBinary, 0o755)
	}
	versions.invalidate()
	running := err == nil && installed != "" && a.systemd && a.unitActive("fastcp-caddy")
	if running {
		// The running Caddy may not load configuration written for the new
		// one, so the next apply restarts it instead of reloading.
		a.caddyRestart = true
		if err := writeFileAtomic(caddyRestartFlag, nil, 0o600); err != nil {
			slog.Warn("could not record the pending web server restart", "error", err)
		}
	}
	a.reloadMu.Unlock()
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "Caddy %s installed\n", CaddyBuild)
	if !running {
		return nil
	}

	if err := a.reapply(ctx); err != nil {
		return fmt.Errorf("applying the web server configuration: %w", err)
	}
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	if a.caddyRestart {
		// No state yet: the configuration on disk suits either build.
		if err := a.systemctl(ctx, "restart", "fastcp-caddy"); err != nil {
			return fmt.Errorf("restarting the web server: %w", err)
		}
		a.caddyRestart = false
		clearCaddyRestart()
	}
	return nil
}

// obtainCaddy moves the staged Caddy build into dir, or downloads it there.
func obtainCaddy(ctx context.Context, log io.Writer, dir string) (string, error) {
	bin := filepath.Join(dir, "caddy")
	if err := os.Rename(StagedCaddy, bin); err == nil {
		if fi, err := os.Lstat(bin); err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", StagedCaddy)
		}
		return bin, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	tag := version.Tag()
	if tag == "dev" {
		return "", fmt.Errorf("a development build cannot download Caddy; build caddy/ into %s", StagedCaddy)
	}
	name := caddyAsset(runtime.GOARCH)
	fmt.Fprintf(log, "Downloading Caddy %s\n", CaddyBuild)
	if err := release.NewClient().Fetch(ctx, tag, dir, []string{name}); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// installedPMAVersion reads the RELEASE-DATE-<version> marker shipped in
// phpMyAdmin's release archives.
func installedPMAVersion() string {
	if _, err := os.Stat(filepath.Join(PMADir, "index.php")); err != nil {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(PMADir, "RELEASE-DATE-*"))
	if len(m) != 1 {
		return "unknown"
	}
	return strings.TrimPrefix(filepath.Base(m[0]), "RELEASE-DATE-")
}

func ensurePMA(ctx context.Context, log io.Writer) error {
	installed := installedPMAVersion()
	if installed == "unknown" {
		installed = "0.0.0"
	}
	if !needsUpgrade(installed, PMAVersion) {
		ensurePMAConfig()
		return nil
	}
	fmt.Fprintf(log, "Downloading phpMyAdmin %s\n", PMAVersion)
	archive, err := downloadVerified(ctx, pmaURL(), pmaSHA256, PrivateTmpDir)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	staged := PMADir + ".new"
	old := PMADir + ".old"
	_ = os.RemoveAll(staged)
	_ = os.RemoveAll(old)
	if err := os.Mkdir(staged, 0o755); err != nil {
		return err
	}
	if err := extractTarGz(archive, staged, 1); err != nil {
		os.RemoveAll(staged)
		return err
	}
	if data, err := os.ReadFile(pmaConfigPath); err == nil {
		if err := os.WriteFile(filepath.Join(staged, "config.inc.php"), data, 0o640); err != nil {
			os.RemoveAll(staged)
			return err
		}
	}
	if _, err := os.Lstat(PMADir); err == nil {
		if err := os.Rename(PMADir, old); err != nil {
			os.RemoveAll(staged)
			return err
		}
	}
	if err := os.Rename(staged, PMADir); err != nil {
		_ = os.Rename(old, PMADir)
		return err
	}
	_ = os.RemoveAll(old)
	ensurePMAConfig()
	fmt.Fprintf(log, "phpMyAdmin %s installed\n", PMAVersion)
	return nil
}

// provisionPHP installs the newest supported PHP version unless it is
// already installed.
func (a *Agent) provisionPHP(ctx context.Context, log io.Writer) error {
	a.aptMu.Lock()
	defer a.aptMu.Unlock()
	if repo, err := ensurePHPRepo(ctx, log); err != nil {
		fmt.Fprintf(log, "Could not add the %s repository: %v\n", repo, err)
	}
	if err := aptLocked(ctx, log, 10*time.Minute, "update"); err != nil {
		return err
	}
	var supported []string
	for _, v := range discoverPHP(ctx).Available {
		if compareBranch(v, minPHPVersion) >= 0 {
			supported = append(supported, v)
		}
	}
	latest := latestBranch(supported)
	if latest == "" {
		return errors.New("no supported PHP version is available from the package repositories")
	}
	if installedPHPBranches()[latest] {
		fmt.Fprintf(log, "PHP %s is already installed\n", latest)
		return nil
	}
	return a.installPHPLocked(ctx, log, latest)
}

// slogWriter sends progress lines to the agent log.
type slogWriter struct{}

func (slogWriter) Write(p []byte) (int, error) {
	if s := strings.TrimSpace(string(p)); s != "" {
		slog.Info(s)
	}
	return len(p), nil
}
