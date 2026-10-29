package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// ProxyURL normalizes the address of the application a reverse proxy site
// forwards requests to. It accepts an http:// or https:// URL with a host and
// an optional port, "host:port", or a bare port on this server. The result
// has no path and contains no Caddyfile syntax.
//
// Loopback addresses on a port in reserved are refused, so a site cannot
// publish FastCP's own services or proxy to the web server itself.
func ProxyURL(raw string, reserved ...int) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("the application address is required")
	}
	if len(s) > 300 {
		return "", errors.New("the application address is too long")
	}
	if _, err := strconv.Atoi(s); err == nil {
		s = "http://127.0.0.1:" + s
	} else if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errors.New("the application address is not a valid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("the application address must start with http:// or https://")
	}
	if u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("the application address can only have a host and a port, such as http://127.0.0.1:3000")
	}

	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", errors.New("the application address needs a host, such as 127.0.0.1")
	}
	loopback := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", fmt.Errorf("%s cannot be used as an application address", host)
		}
		loopback = ip.IsLoopback()
		host = ip.String()
	} else {
		if len(host) > 253 {
			return "", errors.New("the application host name is too long")
		}
		for _, l := range strings.Split(host, ".") {
			if !labelRE.MatchString(l) {
				return "", fmt.Errorf("invalid host name %q", u.Hostname())
			}
		}
	}

	port := 80
	if scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("the port must be between 1 and 65535")
		}
		port = n
	}
	if loopback && slices.Contains(reserved, port) {
		return "", fmt.Errorf("port %d on this server is used by FastCP; run your application on another port", port)
	}

	hostport := host
	if u.Port() != "" {
		hostport = net.JoinHostPort(host, strconv.Itoa(port))
	} else if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	return scheme + "://" + hostport, nil
}
