//go:build !linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
	"github.com/rehmatworks/fastcp/internal/version"
)

// devPassword is accepted for every non-root user by the development agent.
const devPassword = "fastcp-dev"

// devAgent is an in-memory stand-in for the Linux agent, used to develop
// the panel and UI on other operating systems. It changes nothing on the
// host.
type devAgent struct {
	mu        sync.Mutex
	state     State
	users     map[string]bool
	databases map[string]string
	keys      map[string]map[string]string
	caddy     CaddyConfig
	mysql     MySQLConfig
	ssh       SSHConfig
	php       PHPSettings
	installed map[string]bool
	firewall  FirewallStatus
	rclone    bool
	jobs      *jobStore
	ctx       context.Context
}

// Run starts the development agent. It refuses to run unless FASTCP_DEV=1.
func Run(ctx context.Context, socketPath string) error {
	if os.Getenv("FASTCP_DEV") != "1" {
		return errors.New("fastcp-agent only runs on Linux; set FASTCP_DEV=1 to start the in-memory development agent")
	}
	a := &devAgent{
		users:     map[string]bool{},
		databases: map[string]string{},
		keys:      map[string]map[string]string{},
		caddy:     defaultCaddyConfig(4096),
		mysql:     MySQLConfig{BufferPoolMB: 256, MaxConnections: 60, DetectedRAMMB: 4096},
		ssh:       SSHConfig{Port: 22, PasswordAuth: true},
		php:       defaultPHPSettings(),
		installed: map[string]bool{"8.3": true, "8.4": true},
		firewall: FirewallStatus{
			Installed: true, Available: true, SSHPorts: []int{22}, ProtectedPorts: []int{22, 80, 443, PanelPort},
			Rules: []FirewallRule{
				{Port: 22, Protocol: "tcp", Action: "ALLOW", From: "Anywhere", IPVersion: "ipv4", Protected: true, Raw: "22/tcp"},
				{Port: 80, Protocol: "tcp", Action: "ALLOW", From: "Anywhere", IPVersion: "ipv4", Protected: true, Raw: "80/tcp"},
				{Port: 443, Protocol: "tcp", Action: "ALLOW", From: "Anywhere", IPVersion: "ipv4", Protected: true, Raw: "443/tcp"},
				{Port: PanelPort, Protocol: "tcp", Action: "ALLOW", From: "Anywhere", IPVersion: "ipv4", Protected: true, Raw: "2050/tcp"},
			},
		},
		jobs: newJobStore("", nil),
		ctx:  ctx,
	}
	a.php.DefaultVersion = "8.4"

	srv := NewRPCServer()
	uid := uint32(os.Getuid())
	srv.Authorize = func(peer uint32) bool { return peer == uid || peer == 0 }
	a.register(srv)

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	_ = os.Chmod(socketPath, 0o600)
	slog.Warn("running the in-memory development agent; nothing on this machine is changed", "socket", socketPath, "password", devPassword)
	return srv.Serve(ctx, ln)
}

// RunSetup is a no-op outside Linux.
func RunSetup(ctx context.Context) error {
	return errors.New("setup is only supported on Linux")
}

// Provision is only supported on Linux.
func Provision(ctx context.Context, log io.Writer) error {
	return errors.New("provision is only supported on Linux")
}

// FinishUpdate is only supported on Linux.
func FinishUpdate(ctx context.Context, jobID, tag, dir string) error {
	return errors.New("updates are only supported on Linux")
}

func ok() (any, error) { return map[string]string{"status": "ok"}, nil }

func (a *devAgent) register(s *RPCServer) {
	h := func(name string, fn func(json.RawMessage) (any, error)) {
		s.Handle(name, func(_ context.Context, p json.RawMessage) (any, error) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return fn(p)
		})
	}
	h("ping", func(json.RawMessage) (any, error) { return map[string]string{"version": version.Version}, nil })
	h("config.apply", func(p json.RawMessage) (any, error) {
		st, err := decode[State](p)
		if err != nil {
			return nil, err
		}
		plan := buildPlan(st, func(name string) (*tenant, error) {
			return &tenant{Name: name, UID: 1000, GID: 1000, Home: "/home/" + name}, nil
		}, a.installed, a.php.DefaultVersion)
		a.state = *st
		for _, u := range st.Users {
			a.users[u.Username] = true
		}
		return &ApplyResult{Warnings: plan.Warnings}, nil
	})
	h("auth.verify", func(p json.RawMessage) (any, error) {
		req, err := decode[AuthRequest](p)
		if err != nil {
			return nil, err
		}
		if req.Username == "root" || validate.Username(req.Username) != nil || req.Password != devPassword {
			return nil, deniedf("invalid username or password")
		}
		return map[string]bool{"ok": true}, nil
	})
	h("auth.changePassword", func(p json.RawMessage) (any, error) { return ok() })
	h("user.create", func(p json.RawMessage) (any, error) {
		req, err := decode[CreateUserRequest](p)
		if err != nil {
			return nil, err
		}
		if err := validate.NewUsername(req.Username); err != nil {
			return nil, invalid(err)
		}
		if a.users[req.Username] {
			return nil, conflictf("a system account named %s already exists", req.Username)
		}
		a.users[req.Username] = true
		return ok()
	})
	h("user.delete", func(p json.RawMessage) (any, error) {
		req, err := decode[DeleteUserRequest](p)
		if err != nil {
			return nil, err
		}
		delete(a.users, req.Username)
		for _, d := range req.Databases {
			delete(a.databases, d.DBName)
		}
		return ok()
	})
	h("user.exec", func(p json.RawMessage) (any, error) {
		return &UserExecResult{ExitCode: 0, Output: "development agent: command not executed\n"}, nil
	})
	h("backup.prepareSpool", func(p json.RawMessage) (any, error) {
		req, err := decode[SpoolRequest](p)
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(os.TempDir(), "fastcp-dev-spool", req.Username)
		return &SpoolResult{Dir: dir}, os.MkdirAll(dir, 0o700)
	})
	h("site.create", func(p json.RawMessage) (any, error) { return ok() })
	h("site.delete", func(p json.RawMessage) (any, error) { return ok() })
	h("site.readLog", func(p json.RawMessage) (any, error) {
		req, err := decode[SiteLogRequest](p)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		var data string
		switch req.Kind {
		case SiteLogPHP:
			stamp := now.Add(-3 * time.Minute).Format("02-Jan-2006 15:04:05")
			data = fmt.Sprintf("[%s UTC] PHP Warning:  Undefined variable $title in /home/%s/apps/%s/public/index.php on line 12\n", stamp, req.Username, req.Slug)
		case SiteLogAccess:
			if !a.caddy.AccessLogs {
				return &SiteLogResult{}, nil
			}
			for i, r := range []struct {
				uri    string
				status int
			}{{"/", 200}, {"/wp-login.php", 302}, {"/missing", 404}, {"/checkout", 500}} {
				ts := float64(now.Add(time.Duration(i-4)*time.Minute).UnixMilli()) / 1000
				data += fmt.Sprintf(`{"level":"info","ts":%.3f,"msg":"handled request","request":{"remote_ip":"203.0.113.7","client_ip":"203.0.113.7","proto":"HTTP/2.0","method":"GET","host":"example.com","uri":%q,"headers":{"User-Agent":["Mozilla/5.0"]}},"duration":0.0123,"size":5120,"status":%d}`+"\n", ts, r.uri, r.status)
			}
		default:
			return nil, invalid(fmt.Errorf("unknown log %q", req.Kind))
		}
		return &SiteLogResult{Exists: true, Size: int64(len(data)), ModTime: now, Data: data}, nil
	})
	h("site.pageCache", func(p json.RawMessage) (any, error) {
		return &PageCacheStatus{
			Available: true, Plugin: "WP Super Cache", Version: "3.1.4",
			Reasons: []string{"Caching is off in WP Super Cache."},
		}, nil
	})
	h("site.installWordPress", func(p json.RawMessage) (any, error) {
		req, err := decode[InstallWordPressRequest](p)
		if err != nil {
			return nil, err
		}
		a.databases[req.DBName] = req.DBUser
		return map[string]string{"version": "6.8"}, nil
	})
	h("database.create", func(p json.RawMessage) (any, error) {
		req, err := decode[CreateDatabaseRequest](p)
		if err != nil {
			return nil, err
		}
		if _, exists := a.databases[req.DBName]; exists {
			return nil, conflictf("database %s already exists", req.DBName)
		}
		a.databases[req.DBName] = req.DBUser
		return ok()
	})
	h("database.delete", func(p json.RawMessage) (any, error) {
		req, err := decode[DatabaseTarget](p)
		if err != nil {
			return nil, err
		}
		delete(a.databases, req.DBName)
		return ok()
	})
	h("database.resetPassword", func(p json.RawMessage) (any, error) { return ok() })
	h("mysql.getConfig", func(p json.RawMessage) (any, error) { c := a.mysql; return &c, nil })
	h("mysql.setConfig", func(p json.RawMessage) (any, error) {
		req, err := decode[MySQLConfig](p)
		if err != nil {
			return nil, err
		}
		req.DetectedRAMMB = a.mysql.DetectedRAMMB
		a.mysql = *req
		return req, nil
	})
	h("ssh.addKey", func(p json.RawMessage) (any, error) {
		req, err := decode[AddSSHKeyRequest](p)
		if err != nil {
			return nil, err
		}
		key, err := validate.ParseSSHPublicKey(req.PublicKey)
		if err != nil {
			return nil, invalid(err)
		}
		if a.keys[req.Username] == nil {
			a.keys[req.Username] = map[string]string{}
		}
		a.keys[req.Username][req.KeyID] = key.Line
		return map[string]string{"fingerprint": key.Fingerprint}, nil
	})
	h("ssh.removeKey", func(p json.RawMessage) (any, error) {
		req, err := decode[RemoveSSHKeyRequest](p)
		if err != nil {
			return nil, err
		}
		delete(a.keys[req.Username], req.KeyID)
		return ok()
	})
	h("ssh.getConfig", func(p json.RawMessage) (any, error) {
		c := a.ssh
		for _, ks := range a.keys {
			c.KeysInstalled = c.KeysInstalled || len(ks) > 0
		}
		return &c, nil
	})
	h("ssh.setConfig", func(p json.RawMessage) (any, error) {
		req, err := decode[SSHConfig](p)
		if err != nil {
			return nil, err
		}
		if err := validate.Port(req.Port); err != nil {
			return nil, invalid(err)
		}
		a.ssh.Port, a.ssh.PasswordAuth = req.Port, req.PasswordAuth
		return &a.ssh, nil
	})
	h("system.status", func(p json.RawMessage) (any, error) {
		host, _ := os.Hostname()
		return &SystemStatus{
			Hostname: host, OS: "Development (" + runtime.GOOS + ")", Kernel: runtime.GOOS, Architecture: runtime.GOARCH,
			Uptime: 3*86400 + 7200, LoadAverage: [3]float64{0.42, 0.37, 0.31}, CPUCount: runtime.NumCPU(),
			MemoryTotal: 4 << 30, MemoryUsed: 1700 << 20, SwapTotal: 1 << 30, SwapUsed: 64 << 20,
			DiskTotal: 80 << 30, DiskUsed: 23 << 30, MySQLVersion: "MySQL 8.0.41", CaddyVersion: CaddyVersion,
			PHPVersions: sortedKeys(a.installed), AgentVersion: version.Version,
		}, nil
	})
	h("system.services", func(p json.RawMessage) (any, error) {
		list := []ServiceStatus{
			{Name: "fastcp", Description: "FastCP control panel", Active: true, State: "running", Enabled: true},
			{Name: "fastcp-agent", Description: "FastCP agent", Active: true, State: "running", Enabled: true},
			{Name: "fastcp-caddy", Description: "FastCP web server (Caddy)", Active: true, State: "running", Enabled: true},
			{Name: "mysql", Description: "MySQL Community Server", Active: true, State: "running", Enabled: true},
		}
		for _, v := range sortedKeys(a.installed) {
			list = append(list, ServiceStatus{Name: "php" + v + "-fpm", Description: "PHP " + v + " FastCGI Process Manager", Active: true, State: "running", Enabled: true})
		}
		return list, nil
	})
	h("caddy.getConfig", func(p json.RawMessage) (any, error) { c := a.caddy; return &c, nil })
	h("caddy.setConfig", func(p json.RawMessage) (any, error) {
		req, err := decode[CaddyConfig](p)
		if err != nil {
			return nil, err
		}
		cfg, err := normalizeCaddyConfig(*req, 4096)
		if err != nil {
			return nil, invalid(err)
		}
		a.caddy = cfg
		return &cfg, nil
	})
	h("caddy.customStatus", func(p json.RawMessage) (any, error) {
		return customStatus(CaddyCustomDir, nil, nil), nil
	})
	h("caddy.applyCustom", func(p json.RawMessage) (any, error) {
		return customStatus(CaddyCustomDir, nil, nil), nil
	})
	h("firewall.status", func(p json.RawMessage) (any, error) { st := a.firewall; return &st, nil })
	h("firewall.install", func(p json.RawMessage) (any, error) {
		return a.jobs.Start(a.ctx, "firewall.install", "ufw", func(context.Context, *JobLog) (string, error) {
			return "The firewall is already installed.", nil
		}), nil
	})
	h("firewall.setEnabled", func(p json.RawMessage) (any, error) {
		req, err := decode[struct {
			Enabled bool `json:"enabled"`
		}](p)
		if err != nil {
			return nil, err
		}
		a.firewall.Enabled = req.Enabled
		return &a.firewall, nil
	})
	rule := func(action string) func(json.RawMessage) (any, error) {
		return func(p json.RawMessage) (any, error) {
			req, err := decode[FirewallRuleRequest](p)
			if err != nil {
				return nil, err
			}
			if err := validate.Port(req.Port); err != nil {
				return nil, invalid(err)
			}
			proto := normalizeProtocol(req.Protocol)
			for _, pp := range a.firewall.ProtectedPorts {
				if pp == req.Port && action != "ALLOW" {
					return nil, deniedf("port %d is required by FastCP or SSH and cannot be blocked or removed", req.Port)
				}
			}
			kept := a.firewall.Rules[:0]
			for _, r := range a.firewall.Rules {
				if !(r.Port == req.Port && r.Protocol == proto) {
					kept = append(kept, r)
				}
			}
			a.firewall.Rules = kept
			if action != "" {
				a.firewall.Rules = append(a.firewall.Rules, FirewallRule{Port: req.Port, Protocol: proto, Action: action, From: "Anywhere", IPVersion: "ipv4", Raw: req.Protocol})
			}
			sort.Slice(a.firewall.Rules, func(i, j int) bool { return a.firewall.Rules[i].Port < a.firewall.Rules[j].Port })
			return &a.firewall, nil
		}
	}
	h("firewall.allow", rule("ALLOW"))
	h("firewall.deny", rule("DENY"))
	h("firewall.delete", rule(""))
	h("rclone.status", func(p json.RawMessage) (any, error) {
		if a.rclone {
			return &ToolStatus{Installed: true, Version: "1.68.2"}, nil
		}
		return &ToolStatus{}, nil
	})
	h("rclone.install", func(p json.RawMessage) (any, error) {
		a.rclone = true
		return a.jobs.Start(a.ctx, "rclone.install", "rclone", func(context.Context, *JobLog) (string, error) {
			time.Sleep(2 * time.Second)
			return "Installed rclone 1.68.2.", nil
		}), nil
	})
	h("php.status", func(p json.RawMessage) (any, error) {
		now := time.Now()
		st := &PHPStatus{Settings: a.php, Repository: "ondrej/php", Jobs: a.jobs.List("php.", 10)}
		for _, v := range []string{"8.5", "8.4", "8.3", "8.2", "8.1"} {
			info := PHPVersionInfo{Version: v, Available: true, Installed: a.installed[v], Default: v == a.php.DefaultVersion}
			info.Status, info.EOLDate = phpSupportStatus(v, now)
			if info.Installed {
				info.PatchVersion, info.FPMRunning = v+".12", true
			}
			for _, s := range a.state.Sites {
				if s.PHPVersion == v {
					info.Sites++
				}
			}
			st.Versions = append(st.Versions, info)
		}
		st.LastMaintenance = a.jobs.Latest("php.maintenance", "")
		st.NextMaintenance = nextMaintenance(now, a.php.MaintenanceHour, a.php.MaintenanceMinute).Format(time.RFC3339)
		return st, nil
	})
	h("php.install", func(p json.RawMessage) (any, error) {
		req, err := decode[PHPVersionRequest](p)
		if err != nil {
			return nil, err
		}
		if err := validate.PHPVersion(req.Version); err != nil {
			return nil, invalid(err)
		}
		v := req.Version
		return a.jobs.Start(a.ctx, "php.install", v, func(_ context.Context, log *JobLog) (string, error) {
			log.Printf("Installing PHP %s", v)
			time.Sleep(3 * time.Second)
			a.mu.Lock()
			a.installed[v] = true
			a.mu.Unlock()
			return "PHP " + v + " is installed.", nil
		}), nil
	})
	h("php.runMaintenance", func(p json.RawMessage) (any, error) {
		return a.jobs.Start(a.ctx, "php.maintenance", "", func(_ context.Context, log *JobLog) (string, error) {
			log.Printf("Checking for PHP updates")
			time.Sleep(2 * time.Second)
			return "PHP is up to date.", nil
		}), nil
	})
	h("php.setSettings", func(p json.RawMessage) (any, error) {
		req, err := decode[PHPSettings](p)
		if err != nil {
			return nil, err
		}
		if req.DefaultVersion != "" && !a.installed[req.DefaultVersion] {
			return nil, invalidf("PHP %s is not installed", req.DefaultVersion)
		}
		a.php = *req
		return req, nil
	})
	h("job.get", func(p json.RawMessage) (any, error) { return a.jobs.handleGet(a.ctx, p) })
	h("update.apply", func(p json.RawMessage) (any, error) {
		return nil, invalidf("updates are not available in development mode")
	})
	h("update.status", func(p json.RawMessage) (any, error) {
		return &UpdateStatus{CurrentVersion: version.Version, Job: a.jobs.Latest("update", "")}, nil
	})
}
