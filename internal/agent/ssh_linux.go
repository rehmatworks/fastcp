//go:build linux

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/safefs"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	sshdMainConfig  = "/etc/ssh/sshd_config"
	sshdDropInDir   = "/etc/ssh/sshd_config.d"
	sshdFastcpConf  = "/etc/ssh/sshd_config.d/00-fastcp.conf"
	sshdLegacyConf  = "/etc/ssh/sshd_config.d/fastcp.conf"
	maxAuthKeysSize = 1 << 20
)

// keyMarker identifies FastCP-managed lines. FastCP 2 wrote
// "# fastcp:<id>:<name>", FastCP 3 writes "fastcp:<id>" as the key comment.
func keyMarker(id string) string { return "fastcp:" + id }

func lineHasKeyID(line, id string) bool {
	m := keyMarker(id)
	for _, f := range strings.Fields(line) {
		if f == m || strings.HasPrefix(f, m+":") {
			return true
		}
	}
	return false
}

// openSSHDir opens ~/.ssh, creating it when missing. It refuses a .ssh
// that is a symlink or owned by someone else.
func openSSHDir(t *tenant, create bool) (*safefs.Dir, *safefs.Dir, error) {
	home, err := openHome(t)
	if err != nil {
		return nil, nil, err
	}
	var dir *safefs.Dir
	if create {
		dir, err = home.Mkdir(".ssh", 0o700, t.UID, t.GID)
	} else {
		dir, err = home.OpenDir(".ssh")
	}
	if err != nil {
		home.Close()
		return nil, nil, err
	}
	info, err := dir.Stat()
	if err != nil || int(info.UID) != t.UID {
		dir.Close()
		home.Close()
		return nil, nil, deniedf("%s/.ssh is not owned by %s", t.Home, t.Name)
	}
	return home, dir, nil
}

func readAuthorizedKeys(dir *safefs.Dir, uid int) ([]string, error) {
	data, info, err := dir.ReadFile("authorized_keys", maxAuthKeysSize)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if int(info.UID) != uid {
		return nil, deniedf("authorized_keys is not owned by the user")
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeAuthorizedKeys(dir *safefs.Dir, t *tenant, lines []string) error {
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return dir.WriteFile("authorized_keys", []byte(content), 0o600, t.UID, t.GID)
}

func (a *Agent) handleAddSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[AddSSHKeyRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.UUID(req.KeyID); err != nil {
		return nil, invalid(err)
	}
	key, err := validate.ParseSSHPublicKey(req.PublicKey)
	if err != nil {
		return nil, invalid(err)
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	home, dir, err := openSSHDir(t, true)
	if err != nil {
		return nil, err
	}
	defer home.Close()
	defer dir.Close()
	lines, err := readAuthorizedKeys(dir, t.UID)
	if err != nil {
		return nil, err
	}
	keyBody := strings.Join(strings.Fields(key.Line)[:2], " ")
	for _, l := range lines {
		if lineHasKeyID(l, req.KeyID) {
			return nil, conflictf("this key is already installed")
		}
		if f := strings.Fields(l); len(f) >= 2 && f[0]+" "+f[1] == keyBody {
			return nil, conflictf("this key is already installed")
		}
	}
	lines = append(lines, keyBody+" "+keyMarker(req.KeyID))
	if err := writeAuthorizedKeys(dir, t, lines); err != nil {
		return nil, err
	}
	return map[string]string{"fingerprint": key.Fingerprint}, nil
}

func (a *Agent) handleRemoveSSHKey(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[RemoveSSHKeyRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.UUID(req.KeyID); err != nil {
		return nil, invalid(err)
	}
	t, err := lookupTenant(req.Username)
	if err != nil {
		return nil, err
	}
	home, dir, err := openSSHDir(t, false)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{"status": "ok"}, nil
	}
	if err != nil {
		return nil, err
	}
	defer home.Close()
	defer dir.Close()
	lines, err := readAuthorizedKeys(dir, t.UID)
	if err != nil {
		return nil, err
	}
	kept := lines[:0]
	removed := false
	for _, l := range lines {
		if lineHasKeyID(l, req.KeyID) {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if removed {
		if err := writeAuthorizedKeys(dir, t, kept); err != nil {
			return nil, err
		}
	}
	return map[string]string{"status": "ok"}, nil
}

// ---------------------------------------------------------------------------
// sshd configuration

func sshdBinary() (string, error) {
	for _, p := range []string{"/usr/sbin/sshd", "/sbin/sshd", "/usr/local/sbin/sshd"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", notFoundf("The SSH server (openssh-server) is not installed.")
}

// sshdEffective returns `sshd -T` as key -> values (keys lowercased).
func sshdEffective(ctx context.Context) (map[string][]string, error) {
	bin, err := sshdBinary()
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll("/run/sshd", 0o755)
	out, err := run(ctx, 20*time.Second, bin, "-T", "-C", "user=root,host=localhost,addr=127.0.0.1")
	if err != nil {
		out, err = run(ctx, 20*time.Second, bin, "-T")
		if err != nil {
			return nil, err
		}
	}
	cfg := map[string][]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if ok {
			k = strings.ToLower(k)
			cfg[k] = append(cfg[k], strings.TrimSpace(v))
		}
	}
	return cfg, nil
}

// sshPorts returns the ports sshd listens on (22 if unknown).
func sshPorts(ctx context.Context) []int {
	cfg, err := sshdEffective(ctx)
	if err != nil {
		return []int{22}
	}
	seen := map[int]bool{}
	var ports []int
	for _, v := range cfg["port"] {
		if p, err := strconv.Atoi(v); err == nil && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	if len(ports) == 0 {
		return []int{22}
	}
	return ports
}

// anyAuthorizedKeys reports whether root or any tenant can log in with a key.
func (a *Agent) anyAuthorizedKeys() bool {
	hasKey := func(path string) bool {
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				return true
			}
		}
		return false
	}
	if hasKey("/root/.ssh/authorized_keys") {
		return true
	}
	for _, u := range a.currentState().Users {
		if t, err := lookupTenant(u.Username); err == nil && hasKey(filepath.Join(t.Home, ".ssh", "authorized_keys")) {
			return true
		}
	}
	return false
}

func (a *Agent) handleGetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	cfg := &SSHConfig{Port: 22, PasswordAuth: true, KeysInstalled: a.anyAuthorizedKeys()}
	eff, err := sshdEffective(ctx)
	if err != nil {
		return nil, err
	}
	if p := eff["port"]; len(p) > 0 {
		cfg.Port, _ = strconv.Atoi(p[0])
	}
	pw := len(eff["passwordauthentication"]) > 0 && eff["passwordauthentication"][0] == "yes"
	kbd := len(eff["kbdinteractiveauthentication"]) > 0 && eff["kbdinteractiveauthentication"][0] == "yes"
	cfg.PasswordAuth = pw || kbd
	return cfg, nil
}

type fileBackup struct {
	path    string
	existed bool
	data    []byte
	perm    os.FileMode
}

func backupFile(path string) fileBackup {
	b := fileBackup{path: path, perm: 0o644}
	if fi, err := os.Stat(path); err == nil {
		b.perm = fi.Mode().Perm()
	}
	data, err := os.ReadFile(path)
	if err == nil {
		b.existed, b.data = true, data
	}
	return b
}

func (b fileBackup) restore() {
	if b.existed {
		_ = writeFileAtomic(b.path, b.data, b.perm)
	} else {
		_ = os.Remove(b.path)
	}
}

// commentOutPorts disables Port directives in other sshd config files,
// since sshd listens on every configured port.
func commentOutPorts(content []byte) ([]byte, bool) {
	lines := strings.Split(string(content), "\n")
	changed := false
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.EqualFold(f[0], "Port") {
			lines[i] = "# Disabled by FastCP (port is managed in 00-fastcp.conf): " + strings.TrimSpace(line)
			changed = true
		}
	}
	return []byte(strings.Join(lines, "\n")), changed
}

func hasDropInInclude(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.EqualFold(f[0], "Include") && strings.Contains(f[1], "sshd_config.d") {
			return true
		}
	}
	return false
}

func (a *Agent) handleSetSSHConfig(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[SSHConfig](params)
	if err != nil {
		return nil, err
	}
	if err := validate.Port(req.Port); err != nil {
		return nil, invalid(err)
	}
	if req.Port == PanelPort || req.Port == PMAPort || req.Port == 80 || req.Port == 443 {
		return nil, invalidf("port %d is used by FastCP", req.Port)
	}
	if !req.PasswordAuth && !a.anyAuthorizedKeys() {
		return nil, deniedf("add an SSH key for root or a user before disabling password logins, or you would be locked out")
	}
	bin, err := sshdBinary()
	if err != nil {
		return nil, err
	}

	yesNo := map[bool]string{true: "yes", false: "no"}[req.PasswordAuth]
	content := fmt.Sprintf("# Managed by FastCP. Change these settings in the FastCP panel.\nPort %d\nPasswordAuthentication %s\nKbdInteractiveAuthentication %s\n", req.Port, yesNo, yesNo)

	backups := []fileBackup{backupFile(sshdMainConfig), backupFile(sshdFastcpConf), backupFile(sshdLegacyConf)}
	others, _ := filepath.Glob(filepath.Join(sshdDropInDir, "*.conf"))
	for _, f := range others {
		if f != sshdFastcpConf && f != sshdLegacyConf {
			backups = append(backups, backupFile(f))
		}
	}
	rollback := func() {
		for _, b := range backups {
			b.restore()
		}
	}

	main := backups[0]
	if !main.existed {
		return nil, fmt.Errorf("%s not found", sshdMainConfig)
	}
	newMain := main.data
	if !hasDropInInclude(newMain) {
		newMain = append([]byte("Include /etc/ssh/sshd_config.d/*.conf\n"), newMain...)
	}
	newMain, _ = commentOutPorts(newMain)
	if !bytes.Equal(newMain, main.data) {
		if err := writeFileAtomic(sshdMainConfig, newMain, main.perm); err != nil {
			return nil, err
		}
	}
	for _, b := range backups[3:] {
		if updated, changed := commentOutPorts(b.data); changed {
			if err := writeFileAtomic(b.path, updated, b.perm); err != nil {
				rollback()
				return nil, err
			}
		}
	}
	_ = os.MkdirAll(sshdDropInDir, 0o755)
	_ = os.Remove(sshdLegacyConf)
	if err := writeFileAtomic(sshdFastcpConf, []byte(content), 0o644); err != nil {
		rollback()
		return nil, err
	}
	_ = os.MkdirAll("/run/sshd", 0o755)
	if out, err := run(ctx, 20*time.Second, bin, "-t"); err != nil {
		rollback()
		return nil, invalidf("sshd rejected the configuration; nothing was changed%s", tail(out, 800))
	}

	// Allow the new port before sshd moves to it.
	if a.ufwActive(ctx) {
		if _, err := run(ctx, 30*time.Second, "ufw", "allow", fmt.Sprintf("%d/tcp", req.Port)); err != nil {
			rollback()
			return nil, fmt.Errorf("could not open port %d in the firewall: %w", req.Port, err)
		}
	}
	if err := a.restartSSH(ctx); err != nil {
		rollback()
		_ = a.restartSSH(ctx)
		return nil, fmt.Errorf("sshd did not restart and the previous settings were restored: %w", err)
	}
	slog.Info("updated SSH settings", "port", req.Port, "password_auth", req.PasswordAuth)
	return a.handleGetSSHConfig(ctx, nil)
}

// restartSSH applies sshd changes, including systems where ssh is socket
// activated and the listening port lives in ssh.socket.
func (a *Agent) restartSSH(ctx context.Context) error {
	if !a.systemd {
		if _, err := run(ctx, time.Minute, "service", "ssh", "restart"); err != nil {
			_, err = run(ctx, time.Minute, "service", "sshd", "restart")
			return err
		}
		return nil
	}
	if a.unitActive("ssh.socket") {
		if err := a.systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		if err := a.systemctl(ctx, "restart", "ssh.socket"); err != nil {
			return err
		}
		if a.unitActive("ssh.service") {
			return a.systemctl(ctx, "restart", "ssh.service")
		}
		return nil
	}
	for _, svc := range []string{"ssh", "sshd"} {
		if a.unitExists(svc) {
			return a.systemctl(ctx, "restart", svc)
		}
	}
	return errors.New("no SSH service found")
}

func (a *Agent) ufwActive(ctx context.Context) bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	out, err := run(ctx, 20*time.Second, "ufw", "status")
	return err == nil && strings.Contains(out, "Status: active")
}
