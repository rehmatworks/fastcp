package api

import (
	"context"
	"net/http"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/release"
	"github.com/rehmatworks/fastcp/internal/validate"
)

// ---------------------------------------------------------------------------
// PHP

func (s *Server) getPHPSettings(w http.ResponseWriter, r *http.Request) error {
	s.php.invalidate()
	st, err := s.phpStatus(r.Context())
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) savePHPSettings(w http.ResponseWriter, r *http.Request) error {
	var req agent.PHPSettings
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.DefaultVersion != "" {
		if err := validate.PHPVersion(req.DefaultVersion); err != nil {
			return validation(err)
		}
	}
	if req.MaintenanceHour < 0 || req.MaintenanceHour > 23 || req.MaintenanceMinute < 0 || req.MaintenanceMinute > 59 {
		return badRequest("Pick a maintenance time between 00:00 and 23:59.")
	}
	if err := s.agent.SetPHPSettings(r.Context(), &req); err != nil {
		return err
	}
	return s.getPHPSettings(w, r)
}

func (s *Server) installPHP(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Version string `json:"version"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validate.PHPVersion(req.Version); err != nil {
		return validation(err)
	}
	job, err := s.agent.InstallPHP(r.Context(), req.Version)
	if err != nil {
		return err
	}
	s.php.invalidate()
	return accepted(w, job)
}

func (s *Server) runPHPMaintenance(w http.ResponseWriter, r *http.Request) error {
	job, err := s.agent.RunPHPMaintenance(r.Context())
	if err != nil {
		return err
	}
	s.php.invalidate()
	return accepted(w, job)
}

// ---------------------------------------------------------------------------
// Web server, MySQL, SSH

func (s *Server) getWebServer(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.agent.GetCaddyConfig(r.Context())
	if err != nil {
		return err
	}
	return ok(w, cfg)
}

func (s *Server) saveWebServer(w http.ResponseWriter, r *http.Request) error {
	var req agent.CaddyConfig
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := s.agent.SetCaddyConfig(r.Context(), &req); err != nil {
		return err
	}
	return s.getWebServer(w, r)
}

func (s *Server) getCustomCaddy(w http.ResponseWriter, r *http.Request) error {
	st, err := s.agent.CustomCaddyStatus(r.Context())
	if err != nil {
		return err
	}
	return ok(w, s.withSiteDomains(r.Context(), st))
}

func (s *Server) applyCustomCaddy(w http.ResponseWriter, r *http.Request) error {
	st, err := s.agent.ApplyCustomCaddy(r.Context())
	if err != nil {
		return err
	}
	return ok(w, s.withSiteDomains(r.Context(), st))
}

// withSiteDomains names the site each per-site file belongs to.
func (s *Server) withSiteDomains(ctx context.Context, st *agent.CustomCaddyStatus) *agent.CustomCaddyStatus {
	sites, err := s.db.ListAllSites(ctx)
	if err != nil {
		return st
	}
	domains := map[string]string{}
	for _, site := range sites {
		domains[site.Username+"/"+site.Slug] = site.Domain
	}
	for i, f := range st.Files {
		if f.Username != "" {
			st.Files[i].Domain = domains[f.Username+"/"+f.Slug]
		}
	}
	return st
}

func (s *Server) getMySQL(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.agent.GetMySQLConfig(r.Context())
	if err != nil {
		return err
	}
	return ok(w, cfg)
}

func (s *Server) saveMySQL(w http.ResponseWriter, r *http.Request) error {
	var req agent.MySQLConfig
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := s.agent.SetMySQLConfig(r.Context(), &req); err != nil {
		return err
	}
	return s.getMySQL(w, r)
}

func (s *Server) getSSH(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.agent.GetSSHConfig(r.Context())
	if err != nil {
		return err
	}
	return ok(w, cfg)
}

func (s *Server) saveSSH(w http.ResponseWriter, r *http.Request) error {
	var req agent.SSHConfig
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validate.Port(req.Port); err != nil {
		return validation(err)
	}
	if err := s.agent.SetSSHConfig(r.Context(), &req); err != nil {
		return err
	}
	return s.getSSH(w, r)
}

// ---------------------------------------------------------------------------
// Firewall

func (s *Server) getFirewall(w http.ResponseWriter, r *http.Request) error {
	st, err := s.agent.FirewallStatus(r.Context())
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) installFirewall(w http.ResponseWriter, r *http.Request) error {
	job, err := s.agent.InstallFirewall(r.Context())
	if err != nil {
		return err
	}
	return accepted(w, job)
}

func (s *Server) setFirewallEnabled(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := s.agent.SetFirewallEnabled(r.Context(), req.Enabled); err != nil {
		return err
	}
	return s.getFirewall(w, r)
}

type firewallRuleBody struct {
	Action    string `json:"action"` // allow | deny (add only)
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	IPVersion string `json:"ip_version"`
}

func (b *firewallRuleBody) request() (*agent.FirewallRuleRequest, error) {
	if err := validate.Port(b.Port); err != nil {
		return nil, validation(err)
	}
	if b.Protocol == "" {
		b.Protocol = "tcp"
	}
	if b.Protocol != "tcp" && b.Protocol != "udp" {
		return nil, badRequest("Protocol must be tcp or udp.")
	}
	if b.IPVersion == "" {
		b.IPVersion = "both"
	}
	if b.IPVersion != "both" && b.IPVersion != "ipv4" && b.IPVersion != "ipv6" {
		return nil, badRequest("IP version must be both, ipv4 or ipv6.")
	}
	return &agent.FirewallRuleRequest{Port: b.Port, Protocol: b.Protocol, IPVersion: b.IPVersion}, nil
}

func (s *Server) addFirewallRule(w http.ResponseWriter, r *http.Request) error {
	var body firewallRuleBody
	if err := decode(r, &body); err != nil {
		return err
	}
	if body.Action != "allow" && body.Action != "deny" {
		return badRequest("Action must be allow or deny.")
	}
	req, err := body.request()
	if err != nil {
		return err
	}
	if err := s.agent.FirewallRule(r.Context(), body.Action, req); err != nil {
		return err
	}
	return s.getFirewall(w, r)
}

func (s *Server) deleteFirewallRule(w http.ResponseWriter, r *http.Request) error {
	var body firewallRuleBody
	if err := decode(r, &body); err != nil {
		return err
	}
	req, err := body.request()
	if err != nil {
		return err
	}
	if err := s.agent.FirewallRule(r.Context(), "delete", req); err != nil {
		return err
	}
	return s.getFirewall(w, r)
}

// ---------------------------------------------------------------------------
// Updates

type updateInfo struct {
	Current   string    `json:"current_version"`
	Latest    string    `json:"latest_version,omitempty"`
	Available bool      `json:"update_available"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
	NotesURL  string    `json:"release_notes_url,omitempty"`
}

func (s *Server) checkUpdates(ctx context.Context, force bool) *updateInfo {
	if force {
		s.updates.invalidate()
	}
	info, _ := s.updates.get(time.Hour, func() (*updateInfo, error) {
		info := &updateInfo{Current: s.cfg.Version, CheckedAt: time.Now().UTC()}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		latest, err := release.NewClient().Latest(ctx)
		if err != nil {
			info.Error = "Could not reach GitHub to check for updates."
			return info, nil
		}
		info.Latest = latest
		info.NotesURL = "https://github.com/" + release.Repo + "/releases/tag/" + latest
		cur, cerr := validate.ParseSemVer(s.cfg.Version)
		next, nerr := validate.ParseSemVer(latest)
		info.Available = cerr == nil && nerr == nil && next.Compare(cur) > 0
		return info, nil
	})
	return info
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) error {
	info := s.checkUpdates(r.Context(), r.URL.Query().Get("refresh") == "1")
	status, err := s.agent.UpdateStatus(r.Context())
	out := map[string]any{"info": info}
	if err == nil {
		out["agent_version"] = status.CurrentVersion
		out["job"] = status.Job
	}
	return ok(w, out)
}

func (s *Server) applyUpdate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Version string `json:"version"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	v, err := validate.ParseSemVer(req.Version)
	if err != nil {
		return validation(err)
	}
	job, err := s.agent.ApplyUpdate(r.Context(), v.Tag())
	if err != nil {
		return err
	}
	s.updates.invalidate()
	return accepted(w, job)
}

// ---------------------------------------------------------------------------
// rclone

func (s *Server) getRclone(w http.ResponseWriter, r *http.Request) error {
	st, err := s.agent.RcloneStatus(r.Context())
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) installRclone(w http.ResponseWriter, r *http.Request) error {
	job, err := s.agent.InstallRclone(r.Context())
	if err != nil {
		return err
	}
	return accepted(w, job)
}
