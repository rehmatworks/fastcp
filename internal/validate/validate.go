// Package validate holds input checks shared by the panel API and the root
// agent. The agent re-validates every request even when the panel already
// did, so these functions must stay free of side effects.
package validate

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

var (
	usernameRE    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	newUsernameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)
	labelRE       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	slugRE        = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9_-]{0,62}[a-zA-Z0-9])?$`)
	// '-' only appears in names FastCP 2 derived from account names; the
	// agent always quotes identifiers.
	dbIdentRE    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	phpVersionRE = regexp.MustCompile(`^[5-9]\.[0-9]{1,2}$`)
	phpSizeRE    = regexp.MustCompile(`^([0-9]{1,6})([KMG]?)$`)
	semverRE     = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)
	cronFieldRE  = regexp.MustCompile(`^[0-9A-Za-z*,/-]+$`)
	uuidRE       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	snapshotIDRE = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
)

// reservedUsernames are system or service accounts that FastCP must never
// create, delete, or act on behalf of.
var reservedUsernames = map[string]bool{
	"root": true, "admin": true, "administrator": true, "daemon": true, "bin": true,
	"sys": true, "sync": true, "games": true, "man": true, "lp": true, "mail": true,
	"news": true, "uucp": true, "proxy": true, "www-data": true, "backup": true,
	"list": true, "irc": true, "gnats": true, "nobody": true, "nogroup": true,
	"sshd": true, "syslog": true, "messagebus": true, "mysql": true, "caddy": true,
	"fastcp-panel": true, "postfix": true, "ubuntu": true, "debian": true,
	"systemd-network": true, "systemd-resolve": true, "systemd-timesync": true,
	"_apt": true, "lxd": true, "tss": true, "uuidd": true, "tcpdump": true,
	"landscape": true, "pollinate": true, "polkitd": true, "usbmux": true,
	"dnsmasq": true, "fwupd-refresh": true, "ftp": true, "operator": true,
}

// IsReservedUsername reports whether name belongs to a system or service account.
func IsReservedUsername(name string) bool {
	return reservedUsernames[name] || strings.HasPrefix(name, "systemd-")
}

// Username checks the shape of an existing account name. It accepts names
// created by older FastCP versions.
func Username(name string) error {
	if !usernameRE.MatchString(name) {
		return errors.New("invalid username")
	}
	return nil
}

// NewUsername checks a name for an account FastCP is about to create.
func NewUsername(name string) error {
	if !newUsernameRE.MatchString(name) {
		return errors.New("username must be 2-32 characters, start with a lowercase letter, and contain only lowercase letters, digits, '-' or '_'")
	}
	if IsReservedUsername(name) {
		return fmt.Errorf("username %q is reserved", name)
	}
	return nil
}

// TenantUsername checks a name the agent is asked to act on as a hosting
// tenant. It does not consult the system user database.
func TenantUsername(name string) error {
	if err := Username(name); err != nil {
		return err
	}
	if IsReservedUsername(name) {
		return fmt.Errorf("username %q is reserved", name)
	}
	return nil
}

// Domain normalizes and validates a fully qualified host name for a website.
// It lowercases the name and strips one trailing dot.
func Domain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", errors.New("domain is required")
	}
	if len(d) > 253 {
		return "", errors.New("domain is too long")
	}
	if net.ParseIP(d) != nil {
		return "", errors.New("IP addresses cannot be used as site domains")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", errors.New("domain must contain at least one dot, for example example.com")
	}
	for _, l := range labels {
		if !labelRE.MatchString(l) {
			return "", fmt.Errorf("invalid domain label %q", l)
		}
	}
	tld := labels[len(labels)-1]
	if _, err := strconv.Atoi(tld); err == nil {
		return "", errors.New("domain has a numeric top-level label")
	}
	if tld == "localhost" || tld == "invalid" {
		return "", fmt.Errorf(".%s domains cannot be used for sites", tld)
	}
	return d, nil
}

// Slug checks a site directory name. Slugs become path components, so dots
// and separators are not allowed.
func Slug(s string) error {
	if !slugRE.MatchString(s) {
		return errors.New("invalid site slug")
	}
	return nil
}

// DBName checks a MySQL database name.
func DBName(s string) error {
	if len(s) == 0 || len(s) > 64 || !dbIdentRE.MatchString(s) {
		return errors.New("database name must start with a letter, use only letters, digits, '_' or '-', and be at most 64 characters")
	}
	return nil
}

// DBUser checks a MySQL account name.
func DBUser(s string) error {
	if len(s) == 0 || len(s) > 32 || !dbIdentRE.MatchString(s) {
		return errors.New("database user must start with a letter, use only letters, digits, '_' or '-', and be at most 32 characters")
	}
	return nil
}

var (
	reservedDBNames = map[string]bool{"mysql": true, "sys": true, "information_schema": true, "performance_schema": true}
	reservedDBUsers = map[string]bool{
		"root": true, "mysql": true, "debian-sys-maint": true, "mysql.sys": true,
		"mysql.session": true, "mysql.infoschema": true, "sys": true, "admin": true,
	}
)

// IsReservedDBName reports whether a database belongs to the server itself.
func IsReservedDBName(s string) bool { return reservedDBNames[strings.ToLower(s)] }

// IsReservedDBUser reports whether a MySQL account is a server account.
func IsReservedDBUser(s string) bool { return reservedDBUsers[strings.ToLower(s)] }

// PHPVersion checks a PHP minor version such as "8.4".
func PHPVersion(v string) error {
	if !phpVersionRE.MatchString(v) {
		return fmt.Errorf("invalid PHP version %q", v)
	}
	return nil
}

// PHPSize normalizes a php.ini size value such as "256M". Values without a
// unit are bytes. The result must lie within [min, max] bytes.
func PHPSize(raw string, min, max int64) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	m := phpSizeRE.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("invalid size %q; use a number with an optional K, M or G suffix", raw)
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	mult := int64(1)
	switch m[2] {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "G":
		mult = 1 << 30
	}
	bytes := n * mult
	if bytes < min || bytes > max {
		return "", fmt.Errorf("size %s is out of range", v)
	}
	return m[1] + m[2], nil
}

// NoControl rejects strings that contain control characters (including
// newlines and NUL) or invalid UTF-8.
func NoControl(field, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s contains invalid characters", field)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters or line breaks", field)
		}
	}
	return nil
}

// Label checks a short human-readable name such as a cron job or SSH key name.
func Label(field, s string, maxLen int) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(s) > maxLen {
		return fmt.Errorf("%s must be at most %d characters", field, maxLen)
	}
	return NoControl(field, s)
}

// Password checks a password that is passed to chpasswd or PAM.
func Password(s string) error {
	if len(s) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(s) > 128 {
		return errors.New("password must be at most 128 characters")
	}
	return NoControl("password", s)
}

// Port checks a TCP or UDP port number.
func Port(p int) error {
	if p < 1 || p > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

// UUID checks a lowercase canonical UUID string.
func UUID(s string) error {
	if !uuidRE.MatchString(s) {
		return errors.New("invalid id")
	}
	return nil
}

// SnapshotID checks a restic snapshot ID (short or full hex form).
func SnapshotID(s string) error {
	if !snapshotIDRE.MatchString(s) {
		return errors.New("invalid snapshot id")
	}
	return nil
}

var cronMacros = map[string]bool{
	"@hourly": true, "@daily": true, "@weekly": true, "@monthly": true,
	"@yearly": true, "@annually": true, "@midnight": true,
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// CronExpression checks a five-field crontab schedule or a standard macro.
func CronExpression(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return errors.New("schedule is required")
	}
	if cronMacros[expr] {
		return nil
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return errors.New("schedule must have five fields: minute hour day-of-month month day-of-week")
	}
	for _, f := range fields {
		if !cronFieldRE.MatchString(f) {
			return fmt.Errorf("invalid schedule field %q", f)
		}
	}
	if _, err := cronParser.Parse(expr); err != nil {
		return fmt.Errorf("invalid schedule: %v", err)
	}
	return nil
}

// CronJobSchedule checks a cron job's schedule: a CronExpression, or
// @reboot to run the command when the server starts.
func CronJobSchedule(expr string) error {
	if strings.TrimSpace(expr) == "@reboot" {
		return nil
	}
	return CronExpression(expr)
}

// CronCommand checks a command line that will be written to a crontab.
func CronCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return errors.New("command is required")
	}
	if len(cmd) > 2048 {
		return errors.New("command must be at most 2048 characters")
	}
	return NoControl("command", cmd)
}

// EscapeCronPercent escapes '%' for crontab, where an unescaped '%' ends the
// command and starts its standard input. Already-escaped '\%' is kept.
func EscapeCronPercent(cmd string) string {
	var b strings.Builder
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == '%' && (i == 0 || cmd[i-1] != '\\') {
			b.WriteString(`\%`)
			continue
		}
		b.WriteByte(cmd[i])
	}
	return b.String()
}
