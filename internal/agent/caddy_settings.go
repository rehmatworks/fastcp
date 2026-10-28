package agent

import (
	"fmt"
	"strings"
	"time"
)

var caddyProfiles = []string{"low_ram", "balanced", "high_throughput"}

// caddyProfileDefaults returns the timeouts used by a preset profile.
func caddyProfileDefaults(profile string, ramMB int) CaddyConfig {
	switch profile {
	case "high_throughput":
		return CaddyConfig{Profile: profile, ReadHeader: "10s", ReadBody: "45s", WriteTimeout: "120s", IdleTimeout: "240s", GracePeriod: "20s", MaxHeaderSize: 65536}
	case "balanced":
		c := CaddyConfig{Profile: profile, ReadHeader: "10s", ReadBody: "30s", WriteTimeout: "90s", IdleTimeout: "90s", GracePeriod: "8s", MaxHeaderSize: 32768}
		if ramMB > 0 && ramMB <= 2048 {
			c.ReadBody, c.WriteTimeout, c.IdleTimeout, c.GracePeriod, c.MaxHeaderSize = "20s", "60s", "45s", "5s", 16384
		} else if ramMB >= 8192 {
			c.IdleTimeout, c.GracePeriod, c.MaxHeaderSize = "180s", "15s", 65536
		}
		return c
	default:
		return CaddyConfig{Profile: "low_ram", ReadHeader: "8s", ReadBody: "20s", WriteTimeout: "45s", IdleTimeout: "45s", GracePeriod: "5s", MaxHeaderSize: 16384}
	}
}

func defaultCaddyConfig(ramMB int) CaddyConfig {
	return caddyProfileDefaults("low_ram", ramMB)
}

func normalizeDuration(name, v string) (string, error) {
	v = strings.TrimSpace(v)
	d, err := time.ParseDuration(v)
	if err != nil {
		return "", fmt.Errorf("%s must be a duration such as 30s", name)
	}
	if d < time.Second || d > 10*time.Minute {
		return "", fmt.Errorf("%s must be between 1s and 10m", name)
	}
	return d.String(), nil
}

// normalizeCaddyConfig validates cfg. A preset profile always uses its own
// timeouts; "custom" keeps the supplied values after validation.
func normalizeCaddyConfig(cfg CaddyConfig, ramMB int) (CaddyConfig, error) {
	cfg.Profile = strings.ToLower(strings.TrimSpace(cfg.Profile))
	if cfg.Profile == "" {
		cfg.Profile = "low_ram"
	}
	if cfg.Profile != "custom" {
		known := false
		for _, p := range caddyProfiles {
			known = known || p == cfg.Profile
		}
		if !known {
			return CaddyConfig{}, fmt.Errorf("profile must be one of low_ram, balanced, high_throughput or custom")
		}
		out := caddyProfileDefaults(cfg.Profile, ramMB)
		out.AccessLogs = cfg.AccessLogs
		return out, nil
	}
	base := caddyProfileDefaults("balanced", ramMB)
	fields := []struct {
		name string
		val  *string
		def  string
	}{
		{"read_header", &cfg.ReadHeader, base.ReadHeader},
		{"read_body", &cfg.ReadBody, base.ReadBody},
		{"write_timeout", &cfg.WriteTimeout, base.WriteTimeout},
		{"idle_timeout", &cfg.IdleTimeout, base.IdleTimeout},
		{"grace_period", &cfg.GracePeriod, base.GracePeriod},
	}
	for _, f := range fields {
		if strings.TrimSpace(*f.val) == "" {
			*f.val = f.def
		}
		v, err := normalizeDuration(f.name, *f.val)
		if err != nil {
			return CaddyConfig{}, err
		}
		*f.val = v
	}
	if cfg.MaxHeaderSize == 0 {
		cfg.MaxHeaderSize = base.MaxHeaderSize
	}
	if cfg.MaxHeaderSize < 4096 || cfg.MaxHeaderSize > 262144 {
		return CaddyConfig{}, fmt.Errorf("max_header_size must be between 4096 and 262144 bytes")
	}
	return cfg, nil
}
