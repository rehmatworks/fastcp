//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	var errs []error
	if err := a.ensureCaddy(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("installing Caddy %s: %w", CaddyVersion, err))
	}
	if err := ensurePMA(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("installing phpMyAdmin %s: %w", PMAVersion, err))
	}
	return errors.Join(errs...)
}

// upgradeComponents runs ensureComponents in the background of a running
// agent; failures are retried on the next start.
func (a *Agent) upgradeComponents(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := a.ensureComponents(ctx, slogWriter{}); err != nil {
		slog.Warn("could not upgrade bundled components", "error", err)
	}
}

func (a *Agent) ensureCaddy(ctx context.Context, log io.Writer) error {
	installed := ""
	if _, err := os.Stat(CaddyBinary); err == nil {
		installed = caddyVersion(ctx)
		if installed == "" {
			installed = "unknown"
		}
	}
	if !needsUpgrade(installed, CaddyVersion) {
		return nil
	}
	sum, ok := caddySHA256[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("no Caddy build is pinned for %s", runtime.GOARCH)
	}
	fmt.Fprintf(log, "Downloading Caddy %s\n", CaddyVersion)
	archive, err := downloadVerified(ctx, caddyURL(runtime.GOARCH), sum, PrivateTmpDir)
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	work, err := os.MkdirTemp(PrivateTmpDir, "caddy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := extractTarGz(archive, work, 0); err != nil {
		return err
	}
	bin := filepath.Join(work, "caddy")
	if fi, err := os.Lstat(bin); err != nil || !fi.Mode().IsRegular() {
		return errors.New("the archive does not contain the caddy binary")
	}

	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(CaddyBinary), 0o755); err != nil {
		return err
	}
	if err := copyFile(bin, CaddyBinary, 0o755); err != nil {
		return err
	}
	versions.invalidate()
	fmt.Fprintf(log, "Caddy %s installed\n", CaddyVersion)
	if installed != "" && a.systemd && a.unitActive("fastcp-caddy") {
		if err := a.systemctl(ctx, "restart", "fastcp-caddy"); err != nil {
			return fmt.Errorf("restarting the web server: %w", err)
		}
	}
	return nil
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
