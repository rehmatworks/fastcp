package agent

import (
	"bufio"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// minPHPVersion is the oldest branch FastCP offers to install.
const minPHPVersion = "8.1"

// phpSupport lists the end of active support and of security support for
// each PHP branch (https://www.php.net/supported-versions.php).
var phpSupport = map[string][2]string{
	"8.1": {"2023-11-25", "2025-12-31"},
	"8.2": {"2024-12-31", "2026-12-31"},
	"8.3": {"2025-12-31", "2027-12-31"},
	"8.4": {"2026-12-31", "2028-12-31"},
	"8.5": {"2027-12-31", "2029-12-31"},
}

// phpModules are installed with every PHP version when apt offers them.
var phpModules = []string{
	"bcmath", "bz2", "cli", "common", "curl", "gd", "gmp", "igbinary", "imagick",
	"intl", "mbstring", "mysql", "opcache", "readline", "redis", "soap",
	"sqlite3", "xml", "zip",
}

var phpBranchRE = regexp.MustCompile(`^(\d+)\.(\d+)$`)

func parseBranch(v string) (int, int, bool) {
	m := phpBranchRE.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b, true
}

// compareBranch orders "8.4" < "8.10".
func compareBranch(a, b string) int {
	am, an, _ := parseBranch(a)
	bm, bn, _ := parseBranch(b)
	switch {
	case am != bm:
		return cmpInt(am, bm)
	default:
		return cmpInt(an, bn)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func sortBranches(vs []string) {
	sort.Slice(vs, func(i, j int) bool { return compareBranch(vs[i], vs[j]) < 0 })
}

func latestBranch(vs []string) string {
	out := ""
	for _, v := range vs {
		if out == "" || compareBranch(v, out) > 0 {
			out = v
		}
	}
	return out
}

// phpSupportStatus reports "active", "security", "eol" or "unknown" and the
// end-of-life date when known. Branches newer than the table are assumed to
// be in active support.
func phpSupportStatus(version string, now time.Time) (string, string) {
	dates, ok := phpSupport[version]
	if !ok {
		known := make([]string, 0, len(phpSupport))
		for k := range phpSupport {
			known = append(known, k)
		}
		if compareBranch(version, latestBranch(known)) > 0 {
			return "active", ""
		}
		return "unknown", ""
	}
	day := now.UTC().Format("2006-01-02")
	switch {
	case day <= dates[0]:
		return "active", dates[1]
	case day <= dates[1]:
		return "security", dates[1]
	default:
		return "eol", dates[1]
	}
}

var fpmPackageRE = regexp.MustCompile(`^php(\d+\.\d+)-fpm(\s|$)`)

// parseFPMPackages extracts branch numbers from `apt-cache search` or
// `apt-cache pkgnames` output, keeping branches >= minPHPVersion.
func parseFPMPackages(out string) []string {
	seen := map[string]bool{}
	var vs []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := fpmPackageRE.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil || seen[m[1]] || compareBranch(m[1], minPHPVersion) < 0 {
			continue
		}
		seen[m[1]] = true
		vs = append(vs, m[1])
	}
	sortBranches(vs)
	return vs
}

// upstreamVersion turns a Debian package version such as
// "1:8.4.13-1+ubuntu24.04.1+deb.sury.org+1" into "8.4.13".
func upstreamVersion(deb string) string {
	deb = strings.TrimSpace(deb)
	if i := strings.Index(deb, ":"); i >= 0 {
		deb = deb[i+1:]
	}
	if i := strings.IndexAny(deb, "-+~"); i >= 0 {
		deb = deb[:i]
	}
	return deb
}

// debUpstream strips the epoch and Debian revision from a package version:
// "1:8.6.0~rc3-1+ubuntu24.04.1+deb.sury.org+1" becomes "8.6.0~rc3".
func debUpstream(deb string) string {
	deb = strings.TrimSpace(deb)
	if i := strings.Index(deb, ":"); i >= 0 {
		deb = deb[i+1:]
	}
	if i := strings.LastIndex(deb, "-"); i >= 0 {
		deb = deb[:i]
	}
	return deb
}

var prereleaseRE = regexp.MustCompile(`(?i)(~|alpha|beta|rc)`)

// isPrerelease reports whether a package version is an alpha, beta or
// release candidate. The ondrej/sury repositories publish these weeks
// before a PHP release.
func isPrerelease(deb string) bool {
	return prereleaseRE.MatchString(debUpstream(deb))
}

// releasedBranches drops branches whose candidate package is a pre-release.
// A branch without package details is kept only when its support schedule
// is published, which implies a final release.
func releasedBranches(vs []string, policy map[string][2]string) []string {
	var out []string
	for _, v := range vs {
		if cand := policy["php"+v+"-fpm"][1]; cand != "" {
			if isPrerelease(cand) {
				continue
			}
		} else if _, known := phpSupport[v]; !known {
			continue
		}
		out = append(out, v)
	}
	return out
}

// parseAptPolicy returns the installed and candidate versions from
// `apt-cache policy <pkg>` output ("" when absent).
func parseAptPolicy(out string) (installed, candidate string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "Installed:"); ok {
			installed = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, "Candidate:"); ok {
			candidate = strings.TrimSpace(v)
		}
	}
	if installed == "(none)" {
		installed = ""
	}
	if candidate == "(none)" {
		candidate = ""
	}
	return installed, candidate
}

// parseAptPolicyAll splits `apt-cache policy pkg1 pkg2 ...` output into
// package -> [installed, candidate].
func parseAptPolicyAll(out string) map[string][2]string {
	res := map[string][2]string{}
	var pkg string
	var section strings.Builder
	flush := func() {
		if pkg != "" {
			inst, cand := parseAptPolicy(section.String())
			res[pkg] = [2]string{inst, cand}
		}
		section.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		if line != "" && line[0] != ' ' && line[0] != '\t' && strings.HasSuffix(line, ":") {
			flush()
			pkg = strings.TrimSuffix(line, ":")
			continue
		}
		section.WriteString(line)
		section.WriteByte('\n')
	}
	flush()
	return res
}

// parseInstalledPHPPackages parses
// `dpkg-query -W -f '${Package} ${db:Status-Abbrev} ${Version}\n' 'php*'`
// into package name -> version for installed packages.
func parseInstalledPHPPackages(out string) map[string]string {
	pkgs := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !strings.HasPrefix(f[1], "ii") {
			continue
		}
		pkgs[f[0]] = f[len(f)-1]
	}
	return pkgs
}

// nextMaintenance returns the next daily run at hour:minute local time.
func nextMaintenance(now time.Time, hour, minute int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func defaultPHPSettings() PHPSettings {
	return PHPSettings{AutoPatch: true, AutoInstall: true, MaintenanceHour: 3, MaintenanceMinute: 17}
}
