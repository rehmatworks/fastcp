//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

func ufwInstalled() bool {
	_, err := exec.LookPath("ufw")
	return err == nil
}

// protectedPorts are never denied or deleted: losing them would cut off
// the websites, the panel or SSH.
func protectedPorts(ctx context.Context) []int {
	ports := []int{80, 443, PanelPort}
	for _, p := range sshPorts(ctx) {
		if !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	slices.Sort(ports)
	return ports
}

func (a *Agent) handleFirewallStatus(ctx context.Context, params json.RawMessage) (any, error) {
	st := &FirewallStatus{
		Installed:      ufwInstalled(),
		Available:      true,
		SSHPorts:       sshPorts(ctx),
		ProtectedPorts: protectedPorts(ctx),
		Rules:          []FirewallRule{},
	}
	if !st.Installed {
		return st, nil
	}
	out, err := run(ctx, 20*time.Second, "ufw", "status", "numbered")
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "permission denied") || strings.Contains(low, "iptables") {
			st.Available = false
			st.Message = "The firewall cannot be managed in this environment (iptables is not available)."
			return st, nil
		}
		return nil, err
	}
	st.Enabled = ufwStatusActive(out)
	for _, r := range parseUFWNumbered(out) {
		if r.Direction != "IN" {
			continue
		}
		st.Rules = append(st.Rules, FirewallRule{
			Port:      r.Port,
			Protocol:  r.Protocol,
			Action:    r.Action,
			From:      r.From,
			IPVersion: r.IPVersion,
			Protected: r.Action == "ALLOW" && slices.Contains(st.ProtectedPorts, r.Port),
			Raw:       r.To,
		})
	}
	return st, nil
}

func ufwAllow(ctx context.Context, port int, proto string) error {
	_, err := run(ctx, 30*time.Second, "ufw", "allow", fmt.Sprintf("%d/%s", port, proto))
	return err
}

// ensureBaseline opens the ports FastCP needs before the firewall is on.
func ensureBaseline(ctx context.Context) error {
	if _, err := run(ctx, 30*time.Second, "ufw", "default", "deny", "incoming"); err != nil {
		return err
	}
	if _, err := run(ctx, 30*time.Second, "ufw", "default", "allow", "outgoing"); err != nil {
		return err
	}
	for _, p := range protectedPorts(ctx) {
		if err := ufwAllow(ctx, p, "tcp"); err != nil {
			return err
		}
	}
	return ufwAllow(ctx, 443, "udp")
}

func (a *Agent) handleFirewallInstall(ctx context.Context, params json.RawMessage) (any, error) {
	job := a.jobs.Start(a.ctx, "firewall.install", "ufw", func(ctx context.Context, log *JobLog) (string, error) {
		if ufwInstalled() {
			return "The firewall is already installed.", nil
		}
		if err := a.apt(ctx, log, 15*time.Minute, "install", "--no-install-recommends", "ufw"); err != nil {
			return "", err
		}
		if err := ensureBaseline(ctx); err != nil {
			return "", err
		}
		return "The firewall was installed. It stays off until you enable it.", nil
	})
	return job, nil
}

func (a *Agent) handleFirewallSetEnabled(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[struct {
		Enabled bool `json:"enabled"`
	}](params)
	if err != nil {
		return nil, err
	}
	if !ufwInstalled() {
		return nil, notFoundf("the firewall is not installed")
	}
	if req.Enabled {
		if err := ensureBaseline(ctx); err != nil {
			return nil, err
		}
		if _, err := run(ctx, time.Minute, "ufw", "--force", "enable"); err != nil {
			return nil, err
		}
	} else if _, err := run(ctx, time.Minute, "ufw", "disable"); err != nil {
		return nil, err
	}
	return a.handleFirewallStatus(ctx, nil)
}

func firewallRequest(ctx context.Context, params json.RawMessage, guard bool) (*FirewallRuleRequest, error) {
	req, err := decode[FirewallRuleRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.Port(req.Port); err != nil {
		return nil, invalid(err)
	}
	if !ufwInstalled() {
		return nil, notFoundf("the firewall is not installed")
	}
	req.Protocol = normalizeProtocol(req.Protocol)
	req.IPVersion = normalizeIPVersion(req.IPVersion)
	if guard && slices.Contains(protectedPorts(ctx), req.Port) {
		return nil, deniedf("port %d is required by FastCP or SSH and cannot be blocked or removed", req.Port)
	}
	return req, nil
}

func numberedRules(ctx context.Context) ([]ufwRule, error) {
	out, err := run(ctx, 20*time.Second, "ufw", "status", "numbered")
	if err != nil {
		return nil, err
	}
	return parseUFWNumbered(out), nil
}

func deleteRuleNumbers(ctx context.Context, nums []int) error {
	var errs []error
	for _, n := range nums {
		if _, err := run(ctx, 30*time.Second, "ufw", "--force", "delete", strconv.Itoa(n)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addRule adds an allow or deny rule for one or both IP versions,
// replacing opposite rules for the same port.
func addRule(ctx context.Context, action string, req *FirewallRuleRequest) error {
	opposite := "DENY"
	if action == "deny" {
		opposite = "ALLOW"
	}
	rules, err := numberedRules(ctx)
	if err != nil {
		return err
	}
	if err := deleteRuleNumbers(ctx, matchingRules(rules, req.Port, req.Protocol, opposite, req.IPVersion)); err != nil {
		return err
	}
	var args []string
	switch req.IPVersion {
	case "ipv4":
		args = []string{action, "from", "0.0.0.0/0", "to", "any", "port", strconv.Itoa(req.Port), "proto", req.Protocol}
	case "ipv6":
		args = []string{action, "from", "::/0", "to", "any", "port", strconv.Itoa(req.Port), "proto", req.Protocol}
	default:
		args = []string{action, fmt.Sprintf("%d/%s", req.Port, req.Protocol)}
	}
	_, err = run(ctx, 30*time.Second, "ufw", args...)
	return err
}

func (a *Agent) handleFirewallAllow(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := firewallRequest(ctx, params, false)
	if err != nil {
		return nil, err
	}
	if err := addRule(ctx, "allow", req); err != nil {
		return nil, err
	}
	return a.handleFirewallStatus(ctx, nil)
}

func (a *Agent) handleFirewallDeny(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := firewallRequest(ctx, params, true)
	if err != nil {
		return nil, err
	}
	if err := addRule(ctx, "deny", req); err != nil {
		return nil, err
	}
	return a.handleFirewallStatus(ctx, nil)
}

func (a *Agent) handleFirewallDelete(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := firewallRequest(ctx, params, true)
	if err != nil {
		return nil, err
	}
	rules, err := numberedRules(ctx)
	if err != nil {
		return nil, err
	}
	nums := matchingRules(rules, req.Port, req.Protocol, "", req.IPVersion)
	if len(nums) == 0 {
		return nil, notFoundf("no rule for port %d/%s", req.Port, req.Protocol)
	}
	if err := deleteRuleNumbers(ctx, nums); err != nil {
		return nil, err
	}
	return a.handleFirewallStatus(ctx, nil)
}
