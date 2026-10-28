package agent

import (
	"strconv"
	"strings"
)

// ufwRule is one line of `ufw status numbered`.
type ufwRule struct {
	Num       int
	To        string
	Action    string // ALLOW | DENY | REJECT | LIMIT
	Direction string // IN | OUT | FWD
	From      string
	Port      int    // 0 for ranges and application profiles
	Protocol  string // tcp | udp | any
	IPVersion string // ipv4 | ipv6
}

func parseUFWNumbered(out string) []ufwRule {
	var rules []ufwRule
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		end := strings.IndexByte(line, ']')
		if end < 0 {
			continue
		}
		num, err := strconv.Atoi(strings.TrimSpace(line[1:end]))
		if err != nil {
			continue
		}
		parts := strings.Fields(line[end+1:])
		idx := -1
		for i, p := range parts {
			switch p {
			case "ALLOW", "DENY", "REJECT", "LIMIT":
				idx = i
			}
			if idx >= 0 {
				break
			}
		}
		if idx <= 0 {
			continue
		}
		r := ufwRule{Num: num, To: strings.Join(parts[:idx], " "), Action: parts[idx], Direction: "IN"}
		rest := parts[idx+1:]
		if len(rest) > 0 && (rest[0] == "IN" || rest[0] == "OUT" || rest[0] == "FWD") {
			r.Direction = rest[0]
			rest = rest[1:]
		}
		r.From = strings.Join(rest, " ")
		r.IPVersion = "ipv4"
		if strings.Contains(r.To, "(v6)") || strings.Contains(r.From, "(v6)") {
			r.IPVersion = "ipv6"
		}
		r.Port, r.Protocol = parseUFWTarget(parts[0])
		rules = append(rules, r)
	}
	return rules
}

// parseUFWTarget reads "22/tcp", "3306" or "OpenSSH".
func parseUFWTarget(s string) (int, string) {
	portStr, proto, hasProto := strings.Cut(s, "/")
	if !hasProto {
		proto = "any"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 0, proto
	}
	return port, proto
}

func ufwStatusActive(out string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(out)), "status: active")
}

func normalizeProtocol(p string) string {
	if strings.EqualFold(strings.TrimSpace(p), "udp") {
		return "udp"
	}
	return "tcp"
}

func normalizeIPVersion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ipv4", "ipv6":
		return strings.ToLower(strings.TrimSpace(v))
	}
	return "both"
}

// matchingRules returns rule numbers for port/protocol (and optionally
// action and IP version), highest first so deleting them in order keeps
// the remaining numbers valid.
func matchingRules(rules []ufwRule, port int, protocol, action, ipVersion string) []int {
	var nums []int
	for _, r := range rules {
		if r.Port != port || r.Direction != "IN" {
			continue
		}
		if r.Protocol != protocol && r.Protocol != "any" {
			continue
		}
		if action != "" && r.Action != action {
			continue
		}
		if ipVersion != "both" && r.IPVersion != ipVersion {
			continue
		}
		nums = append(nums, r.Num)
	}
	for i, j := 0, len(nums)-1; i < j; i, j = i+1, j-1 {
		nums[i], nums[j] = nums[j], nums[i]
	}
	return nums
}
