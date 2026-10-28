package validate

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var cacheFlagDirectives = map[string]bool{
	"public": true, "private": true, "no-cache": true, "no-store": true,
	"no-transform": true, "must-revalidate": true, "proxy-revalidate": true,
	"must-understand": true, "immutable": true,
}

var cacheSecondsDirectives = map[string]bool{
	"max-age": true, "s-maxage": true, "stale-while-revalidate": true, "stale-if-error": true,
}

// CacheControl normalizes a Cache-Control header value. Only standard
// response directives are accepted; anything else is rejected so the value
// can be embedded in a Caddyfile without being interpreted as a placeholder
// or directive.
func CacheControl(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Cache-Control value is required")
	}
	if len(raw) > 256 {
		return "", errors.New("Cache-Control value is too long")
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		name, value, hasValue := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		switch {
		case cacheFlagDirectives[name] && !hasValue:
			out = append(out, name)
		case cacheSecondsDirectives[name] && hasValue:
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return "", fmt.Errorf("%s needs a whole number of seconds", name)
			}
			out = append(out, fmt.Sprintf("%s=%d", name, n))
		default:
			return "", fmt.Errorf("unsupported Cache-Control directive %q", part)
		}
		if seen[name] {
			return "", fmt.Errorf("duplicate Cache-Control directive %q", name)
		}
		seen[name] = true
	}
	if len(out) == 0 {
		return "", errors.New("Cache-Control value is required")
	}
	return strings.Join(out, ", "), nil
}
