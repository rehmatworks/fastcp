//go:build linux

package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	ondrejFingerprint = "14AA40EC0831756756D7F66C4F4EA0AAE5267A6C"
	ondrejKeyring     = "/etc/apt/keyrings/ondrej-php.asc"
	suryKeyring       = "/etc/apt/keyrings/sury-php.gpg"
	phpSourceFile     = "/etc/apt/sources.list.d/fastcp-php.list"
	legacyPHPDefaults = "/opt/fastcp/config/php-defaults.json"
	phpCacheTTL       = 10 * time.Minute
)

const fpmIni = `; Managed by FastCP. Per-site limits are set in each pool.
expose_php = Off
display_errors = Off
log_errors = On
error_reporting = 22527
`

// ---------------------------------------------------------------------------
// Settings

func loadPHPSettings() PHPSettings {
	s := defaultPHPSettings()
	data, err := os.ReadFile(PHPSettingsPath)
	if err == nil {
		if json.Unmarshal(data, &s) == nil {
			return s
		}
	}
	// First run: pick a random maintenance time so servers do not all hit
	// the package mirrors at once, and keep the FastCP 2 default version.
	if h, err := rand.Int(rand.Reader, big.NewInt(4)); err == nil {
		s.MaintenanceHour = 1 + int(h.Int64())
	}
	if m, err := rand.Int(rand.Reader, big.NewInt(60)); err == nil {
		s.MaintenanceMinute = int(m.Int64())
	}
	if old, err := os.ReadFile(legacyPHPDefaults); err == nil {
		var legacy struct {
			DefaultPHPVersion string `json:"default_php_version"`
		}
		if json.Unmarshal(old, &legacy) == nil && validate.PHPVersion(legacy.DefaultPHPVersion) == nil {
			s.DefaultVersion = legacy.DefaultPHPVersion
		}
	}
	if err := savePHPSettings(s); err != nil {
		slog.Warn("could not save PHP settings", "error", err)
	}
	return s
}

func savePHPSettings(s PHPSettings) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := os.MkdirAll(filepath.Dir(PHPSettingsPath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(PHPSettingsPath, data, 0o644)
}

// effectiveDefaultPHP is the configured default if installed, otherwise
// the newest installed branch.
func effectiveDefaultPHP(s PHPSettings, installed map[string]bool) string {
	if s.DefaultVersion != "" && installed[s.DefaultVersion] {
		return s.DefaultVersion
	}
	if latest := latestBranch(sortedKeys(installed)); latest != "" {
		return latest
	}
	if s.DefaultVersion != "" {
		return s.DefaultVersion
	}
	return "8.4"
}

// installedPHPBranches lists PHP versions with php-fpm installed.
func installedPHPBranches() map[string]bool {
	out := map[string]bool{}
	matches, _ := filepath.Glob("/usr/sbin/php-fpm*")
	for _, m := range matches {
		v := strings.TrimPrefix(filepath.Base(m), "php-fpm")
		if _, _, ok := parseBranch(v); ok {
			out[v] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Package repository

func osRelease() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[k] = strings.Trim(v, `"'`)
		}
	}
	return out
}

// phpRepoConfigured reports an existing ondrej/sury source, whoever added it.
func phpRepoConfigured() (string, bool) {
	files := []string{"/etc/apt/sources.list"}
	for _, pat := range []string{"/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"} {
		m, _ := filepath.Glob(pat)
		files = append(files, m...)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			switch {
			case strings.Contains(line, "ondrej/php"):
				return "ondrej/php", true
			case strings.Contains(line, "packages.sury.org/php"):
				return "packages.sury.org/php", true
			}
		}
	}
	return "", false
}

var codenameRE = regexp.MustCompile(`^[a-z]{3,20}$`)

// verifyKeyFingerprint checks an OpenPGP key file with gpg when available.
func verifyKeyFingerprint(ctx context.Context, path, want string) error {
	if _, err := exec.LookPath("gpg"); err != nil {
		slog.Warn("gpg not found; trusting the key fetched over HTTPS", "key", path)
		return nil
	}
	out, err := runEnv(ctx, 30*time.Second, []string{"GNUPGHOME=" + PrivateTmpDir}, nil, "gpg", "--batch", "--show-keys", "--with-colons", path)
	if err != nil {
		return fmt.Errorf("could not read the repository key: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Split(line, ":"); len(f) > 9 && f[0] == "fpr" && strings.EqualFold(f[9], want) {
			return nil
		}
	}
	return fmt.Errorf("the repository key does not have the expected fingerprint %s", want)
}

// ensurePHPRepo adds the upstream PHP repository for this distribution
// unless one is configured. Callers run apt-get update afterwards.
func ensurePHPRepo(ctx context.Context, log io.Writer) (string, error) {
	if desc, ok := phpRepoConfigured(); ok {
		return desc, nil
	}
	osr := osRelease()
	codename := osr["UBUNTU_CODENAME"]
	if codename == "" {
		codename = osr["VERSION_CODENAME"]
	}
	if !codenameRE.MatchString(codename) {
		return "distribution", fmt.Errorf("could not determine the distribution codename")
	}
	if err := os.MkdirAll("/etc/apt/keyrings", 0o755); err != nil {
		return "distribution", err
	}
	_ = os.MkdirAll(PrivateTmpDir, 0o700)
	switch osr["ID"] {
	case "ubuntu":
		fmt.Fprintln(log, "Adding the ondrej/php repository")
		key, err := httpGet(ctx, "https://keyserver.ubuntu.com/pks/lookup?op=get&options=mr&search=0x"+ondrejFingerprint, 1<<20)
		if err != nil {
			return "distribution", err
		}
		if !strings.Contains(string(key), "BEGIN PGP PUBLIC KEY BLOCK") {
			return "distribution", errors.New("the key server returned an unexpected response")
		}
		tmp := filepath.Join(PrivateTmpDir, "ondrej-php.asc")
		if err := writeFileAtomic(tmp, key, 0o644); err != nil {
			return "distribution", err
		}
		defer os.Remove(tmp)
		if err := verifyKeyFingerprint(ctx, tmp, ondrejFingerprint); err != nil {
			return "distribution", err
		}
		if err := writeFileAtomic(ondrejKeyring, key, 0o644); err != nil {
			return "distribution", err
		}
		src := fmt.Sprintf("deb [signed-by=%s] https://ppa.launchpadcontent.net/ondrej/php/ubuntu %s main\n", ondrejKeyring, codename)
		return "ondrej/php", writeFileAtomic(phpSourceFile, []byte(src), 0o644)
	case "debian":
		fmt.Fprintln(log, "Adding the packages.sury.org/php repository")
		key, err := httpGet(ctx, "https://packages.sury.org/php/apt.gpg", 1<<20)
		if err != nil {
			return "distribution", err
		}
		if err := writeFileAtomic(suryKeyring, key, 0o644); err != nil {
			return "distribution", err
		}
		src := fmt.Sprintf("deb [signed-by=%s] https://packages.sury.org/php/ %s main\n", suryKeyring, codename)
		return "packages.sury.org/php", writeFileAtomic(phpSourceFile, []byte(src), 0o644)
	}
	return "distribution", nil
}

// ---------------------------------------------------------------------------
// Discovery

type phpDiscovery struct {
	Available []string
	Policy    map[string][2]string
	Repo      string
	RepoErr   string
}

func discoverPHP(ctx context.Context) *phpDiscovery {
	d := &phpDiscovery{Policy: map[string][2]string{}, Repo: "distribution"}
	if desc, ok := phpRepoConfigured(); ok {
		d.Repo = desc
	}
	out, err := run(ctx, time.Minute, "apt-cache", "search", "--names-only", `^php[0-9]+\.[0-9]+-fpm$`)
	if err != nil {
		d.RepoErr = "could not query the package index"
	}
	d.Available = parseFPMPackages(out)
	names := map[string]bool{}
	for _, v := range d.Available {
		names["php"+v+"-fpm"] = true
	}
	for v := range installedPHPBranches() {
		names["php"+v+"-fpm"] = true
	}
	if len(names) > 0 {
		args := append([]string{"policy"}, sortedKeys(names)...)
		if out, err := run(ctx, time.Minute, "apt-cache", args...); err == nil {
			d.Policy = parseAptPolicyAll(out)
		}
	}
	d.Available = releasedBranches(d.Available, d.Policy)
	return d
}

func (a *Agent) phpDiscovery(ctx context.Context, refresh bool) *phpDiscovery {
	a.phpMu.Lock()
	defer a.phpMu.Unlock()
	if refresh || a.phpCache == nil || time.Since(a.phpCachedAt) > phpCacheTTL {
		a.phpCache = discoverPHP(ctx)
		a.phpCachedAt = time.Now()
	}
	return a.phpCache
}

func (a *Agent) invalidatePHPCache() {
	a.phpMu.Lock()
	a.phpCache = nil
	a.phpMu.Unlock()
	versions.invalidate()
}

func (a *Agent) handlePHPStatus(ctx context.Context, params json.RawMessage) (any, error) {
	d := a.phpDiscovery(ctx, false)
	installed := installedPHPBranches()
	settings := loadPHPSettings()
	def := effectiveDefaultPHP(settings, installed)
	sites := map[string]int{}
	for _, s := range a.currentState().Sites {
		v := s.PHPVersion
		if !installed[v] {
			v = def
		}
		sites[v]++
	}
	all := map[string]bool{}
	for _, v := range d.Available {
		all[v] = true
	}
	for v := range installed {
		all[v] = true
	}
	list := sortedKeys(all)
	sortBranches(list)
	now := time.Now()
	st := &PHPStatus{Settings: settings, Repository: d.Repo, RepositoryError: d.RepoErr, Jobs: a.jobs.List("php.", 10)}
	for i := len(list) - 1; i >= 0; i-- {
		v := list[i]
		pol := d.Policy["php"+v+"-fpm"]
		info := PHPVersionInfo{
			Version:   v,
			Installed: installed[v],
			Available: slicesContains(d.Available, v),
			Default:   v == def && installed[v],
			Sites:     sites[v],
		}
		info.Status, info.EOLDate = phpSupportStatus(v, now)
		if pol[0] != "" {
			info.PatchVersion = upstreamVersion(pol[0])
		}
		if pol[1] != "" && pol[1] != pol[0] {
			info.Candidate = upstreamVersion(pol[1])
		}
		if info.Installed {
			info.FPMRunning = a.unitActive(fpmService(v)) || (!a.systemd && fpmMasterPID(v) > 0)
		}
		st.Versions = append(st.Versions, info)
	}
	st.LastMaintenance = a.jobs.Latest("php.maintenance", "")
	if settings.AutoPatch || settings.AutoInstall {
		st.NextMaintenance = nextMaintenance(now, settings.MaintenanceHour, settings.MaintenanceMinute).Format(time.RFC3339)
	}
	if st.Versions == nil {
		st.Versions = []PHPVersionInfo{}
	}
	return st, nil
}

func slicesContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Install and maintenance

// phpPackages returns php<v>-fpm plus every common module apt offers.
func phpPackages(ctx context.Context, version string) ([]string, error) {
	out, err := run(ctx, time.Minute, "apt-cache", "pkgnames", "php"+version+"-")
	if err != nil && out == "" {
		return nil, err
	}
	avail := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		avail[strings.TrimSpace(line)] = true
	}
	if !avail["php"+version+"-fpm"] {
		return nil, notFoundf("PHP %s is not available from the configured package repositories", version)
	}
	pkgs := []string{"php" + version + "-fpm"}
	for _, m := range phpModules {
		if p := "php" + version + "-" + m; avail[p] {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, nil
}

func writeFPMIni(version string) {
	dir := filepath.Join("/etc/php", version, "fpm", "conf.d")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if _, err := writeIfChanged(filepath.Join(dir, "99-fastcp.ini"), []byte(fpmIni), 0o644); err != nil {
		slog.Warn("could not write PHP ini overrides", "version", version, "error", err)
	}
}

// installPHPLocked installs a PHP branch. The caller holds aptMu and has
// refreshed the package index.
func (a *Agent) installPHPLocked(ctx context.Context, log io.Writer, version string) error {
	pkgs, err := phpPackages(ctx, version)
	if err != nil {
		return err
	}
	if out, err := run(ctx, time.Minute, "apt-cache", "policy", "php"+version+"-fpm"); err == nil {
		if _, cand := parseAptPolicy(out); isPrerelease(cand) {
			return invalidf("PHP %s is not released yet; the repository only has the pre-release %s", version, debUpstream(cand))
		}
	}
	fmt.Fprintf(log, "Installing PHP %s (%d packages)\n", version, len(pkgs))
	if err := aptLocked(ctx, log, 30*time.Minute, append([]string{"install", "--no-install-recommends"}, pkgs...)...); err != nil {
		return err
	}
	writeFPMIni(version)
	if a.systemd {
		_ = a.systemctl(ctx, "enable", "--now", fpmService(version))
		_ = a.systemctl(ctx, "reload-or-restart", fpmService(version))
	} else {
		_ = directFPMReload(ctx, version)
	}
	return nil
}

func (a *Agent) handlePHPInstall(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[PHPVersionRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.PHPVersion(req.Version); err != nil {
		return nil, invalid(err)
	}
	if compareBranch(req.Version, minPHPVersion) < 0 {
		return nil, invalidf("PHP versions older than %s are not supported", minPHPVersion)
	}
	if installedPHPBranches()[req.Version] {
		return nil, conflictf("PHP %s is already installed", req.Version)
	}
	return a.jobs.Start(a.ctx, "php.install", req.Version, func(ctx context.Context, log *JobLog) (string, error) {
		a.aptMu.Lock()
		defer a.aptMu.Unlock()
		if repo, err := ensurePHPRepo(ctx, log); err != nil {
			log.Printf("Could not add the %s repository: %v", repo, err)
		}
		if err := aptLocked(ctx, log, 10*time.Minute, "update"); err != nil {
			return "", err
		}
		if err := a.installPHPLocked(ctx, log, req.Version); err != nil {
			return "", err
		}
		a.invalidatePHPCache()
		if err := a.reapply(ctx); err != nil {
			log.Printf("Re-applying the configuration failed: %v", err)
		}
		return fmt.Sprintf("PHP %s is installed.", req.Version), nil
	}), nil
}

func (a *Agent) handlePHPMaintenance(ctx context.Context, params json.RawMessage) (any, error) {
	return a.startMaintenance("manual"), nil
}

func (a *Agent) startMaintenance(trigger string) *Job {
	return a.jobs.Start(a.ctx, "php.maintenance", "", func(ctx context.Context, log *JobLog) (string, error) {
		return a.phpMaintenance(ctx, log, trigger == "manual")
	})
}

// phpMaintenance applies patch releases to installed PHP branches and,
// when enabled, installs newly released branches.
func (a *Agent) phpMaintenance(ctx context.Context, log *JobLog, manual bool) (string, error) {
	settings := loadPHPSettings()
	a.aptMu.Lock()
	defer a.aptMu.Unlock()

	if repo, err := ensurePHPRepo(ctx, log); err != nil {
		log.Printf("Could not add the %s repository: %v", repo, err)
	}
	if err := aptLocked(ctx, log, 10*time.Minute, "update"); err != nil {
		return "", err
	}
	var summary []string

	if settings.AutoPatch || manual {
		out, _ := run(ctx, time.Minute, "dpkg-query", "-W", "-f", "${Package} ${db:Status-Abbrev} ${Version}\n", "php*")
		before := parseInstalledPHPPackages(out)
		names := make([]string, 0, len(before))
		for name := range before {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			args := append([]string{"install", "--only-upgrade", "--no-install-recommends"}, names...)
			if err := aptLocked(ctx, log, 30*time.Minute, args...); err != nil {
				return "", err
			}
			out, _ = run(ctx, time.Minute, "dpkg-query", "-W", "-f", "${Package} ${db:Status-Abbrev} ${Version}\n", "php*")
			after := parseInstalledPHPPackages(out)
			upgraded := 0
			for name, v := range before {
				if after[name] != "" && after[name] != v {
					upgraded++
				}
			}
			if upgraded > 0 {
				summary = append(summary, fmt.Sprintf("updated %d PHP packages", upgraded))
			}
		}
	}

	installed := installedPHPBranches()
	if settings.AutoInstall {
		d := discoverPHP(ctx)
		latestInstalled := latestBranch(sortedKeys(installed))
		for _, v := range d.Available {
			if installed[v] || (latestInstalled != "" && compareBranch(v, latestInstalled) <= 0) {
				continue
			}
			if status, _ := phpSupportStatus(v, time.Now()); status == "eol" {
				continue
			}
			if err := a.installPHPLocked(ctx, log, v); err != nil {
				log.Printf("Installing PHP %s failed: %v", v, err)
				continue
			}
			summary = append(summary, "installed PHP "+v)
		}
		installed = installedPHPBranches()
	}

	if settings.FollowLatestDefault {
		if latest := latestBranch(sortedKeys(installed)); latest != "" && latest != settings.DefaultVersion {
			settings.DefaultVersion = latest
			if err := savePHPSettings(settings); err == nil {
				summary = append(summary, "default PHP is now "+latest)
			}
		}
	}

	for v := range installed {
		writeFPMIni(v)
		if out, err := fpmTest(ctx, v); err != nil {
			log.Printf("PHP %s configuration test failed: %s", v, out)
			continue
		}
		if a.systemd && !a.unitActive(fpmService(v)) {
			_ = a.systemctl(ctx, "enable", "--now", fpmService(v))
		}
	}
	a.invalidatePHPCache()
	if err := a.reapply(ctx); err != nil {
		return "", fmt.Errorf("PHP was updated but re-applying the configuration failed: %w", err)
	}
	if len(summary) == 0 {
		return "PHP is up to date.", nil
	}
	msg := strings.Join(summary, "; ")
	return strings.ToUpper(msg[:1]) + msg[1:] + ".", nil
}

func (a *Agent) handlePHPSetSettings(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[PHPSettings](params)
	if err != nil {
		return nil, err
	}
	if req.MaintenanceHour < 0 || req.MaintenanceHour > 23 || req.MaintenanceMinute < 0 || req.MaintenanceMinute > 59 {
		return nil, invalidf("maintenance time must be a valid hour and minute")
	}
	installed := installedPHPBranches()
	if req.DefaultVersion != "" && !installed[req.DefaultVersion] {
		return nil, invalidf("PHP %s is not installed", req.DefaultVersion)
	}
	old := loadPHPSettings()
	if err := savePHPSettings(*req); err != nil {
		return nil, err
	}
	if effectiveDefaultPHP(old, installed) != effectiveDefaultPHP(*req, installed) {
		if err := a.reapply(ctx); err != nil {
			_ = savePHPSettings(old)
			return nil, err
		}
	}
	return req, nil
}

// phpScheduler runs maintenance once a day at the configured time. A run
// missed by up to six hours (agent restart, reboot) is caught up.
func (a *Agent) phpScheduler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s := loadPHPSettings()
			if !s.AutoPatch && !s.AutoInstall {
				continue
			}
			slot := time.Date(now.Year(), now.Month(), now.Day(), s.MaintenanceHour, s.MaintenanceMinute, 0, 0, now.Location())
			if now.Before(slot) || now.After(slot.Add(6*time.Hour)) {
				continue
			}
			if last := a.jobs.Latest("php.maintenance", ""); last != nil {
				if started, err := time.Parse(time.RFC3339Nano, last.StartedAt); err == nil && !started.Before(slot) {
					continue
				}
			}
			slog.Info("starting scheduled PHP maintenance")
			a.startMaintenance("scheduled")
		}
	}
}
