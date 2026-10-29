package api

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestSiteLogs(t *testing.T) {
	h := newHarness(t)
	admin := h.login("fastcp", "admin-pass")
	r := h.do("POST", "/api/sites", admin, map[string]any{"domain": "example.com"})
	expect(t, r, 201, "create site")
	siteID := r.body["id"].(string)

	r = h.do("GET", "/api/sites/"+siteID+"/logs/php", admin, nil)
	expect(t, r, 200, "empty php log")
	if r.body["exists"] != false || len(r.body["lines"].([]any)) != 0 || r.body["path"] != "~/apps/example_com/logs/php-error.log" {
		t.Fatalf("empty php log = %v", r.body)
	}

	h.agent.mu.Lock()
	h.agent.logs["fastcp/example_com/php"] = "[29-Oct-2026 08:00:00 UTC] PHP Warning:  one\n[29-Oct-2026 08:01:00 UTC] PHP Fatal error:  two\n"
	h.agent.logs["fastcp/example_com/access"] = strings.Join([]string{
		`{"ts":1793260800.5,"request":{"remote_ip":"203.0.113.7","client_ip":"203.0.113.7","proto":"HTTP/2.0","method":"GET","host":"example.com","uri":"/first","headers":{"User-Agent":["curl/8"]}},"duration":0.01234,"size":512,"status":200}`,
		`not json`,
		`{"ts":1793260801,"request":{"remote_ip":"104.16.0.1","client_ip":"104.16.0.1","proto":"HTTP/1.1","method":"POST","host":"example.com","uri":"/second","headers":{"Cf-Connecting-Ip":["198.51.100.9"]}},"duration":1.5,"size":0,"status":502}`,
		`{"ts":1793260802,"request":{"remote_ip":"192.0.2.1","client_ip":"192.0.2.1","proto":"HTTP/1.1","method":"GET","host":"example.com","uri":"/spoofed","headers":{"Cf-Connecting-Ip":["198.51.100.10"]}},"duration":0,"size":0,"status":404}`,
	}, "\n") + "\n"
	h.agent.mu.Unlock()

	r = h.do("GET", "/api/sites/"+siteID+"/logs/php", admin, nil)
	expect(t, r, 200, "php log")
	lines := r.body["lines"].([]any)
	if r.body["exists"] != true || len(lines) != 2 || !strings.HasSuffix(lines[1].(string), "two") {
		t.Fatalf("php log = %v", r.body)
	}

	r = h.do("GET", "/api/sites/"+siteID+"/logs/access", admin, nil)
	expect(t, r, 200, "access log")
	if r.body["enabled"] != false {
		t.Fatalf("access logs reported enabled: %v", r.body)
	}
	entries := r.body["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("entries = %v", entries)
	}
	newest, cf, oldest := entries[0].(map[string]any), entries[1].(map[string]any), entries[2].(map[string]any)
	if newest["uri"] != "/spoofed" || newest["ip"] != "192.0.2.1" {
		t.Errorf("CF-Connecting-IP trusted from a non-Cloudflare address: %v", newest)
	}
	if cf["ip"] != "198.51.100.9" || cf["status"] != float64(502) || cf["method"] != "POST" {
		t.Errorf("Cloudflare entry = %v", cf)
	}
	if oldest["uri"] != "/first" || oldest["duration_ms"] != 12.3 || oldest["user_agent"] != "curl/8" || oldest["time"] != "2026-10-29T08:00:00.5Z" {
		t.Errorf("oldest entry = %v", oldest)
	}

	expect(t, h.do("GET", "/api/sites/"+siteID+"/logs/other", admin, nil), 404, "unknown log")
	expect(t, h.do("POST", "/api/users", admin, map[string]any{"username": "alice", "password": "alice-password"}), 201, "create user")
	alice := h.login("alice", "alice-password")
	expect(t, h.do("GET", "/api/sites/"+siteID+"/logs/php", alice, nil), 404, "other user's log")
}

func TestCloudflareDetection(t *testing.T) {
	for addr, want := range map[string]bool{
		"104.16.132.229":    true,
		"172.67.1.1":        true,
		"2606:4700::6810":   true,
		"::ffff:104.16.0.1": true,
		"8.8.8.8":           false,
		"2001:db8::1":       false,
	} {
		if got := isCloudflare(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isCloudflare(%s) = %v", addr, got)
		}
	}

	answers := map[string][]string{
		"proxied.com": {"104.16.132.229", "2606:4700::6810"},
		"mixed.com":   {"104.16.132.229", "203.0.113.5"},
		"direct.com":  {"203.0.113.5"},
	}
	looked := map[string]bool{}
	c := newSSLChecker()
	c.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		looked[host] = true
		var out []netip.Addr
		for _, a := range answers[host] {
			out = append(out, netip.MustParseAddr(a))
		}
		if out == nil {
			return nil, errors.New("no such host")
		}
		return out, nil
	}
	for domain, want := range map[string]string{"proxied.com": proxyCloudflare, "mixed.com": "", "direct.com": "", "missing.com": "", "site.test": ""} {
		if got := c.proxyOf(context.Background(), domain); got != want {
			t.Errorf("proxyOf(%s) = %q, want %q", domain, got, want)
		}
	}
	if looked["site.test"] {
		t.Error("local domains must not be looked up")
	}

	// A server that closes the connection stands in for one without a
	// certificate for the domain.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	c.addr = ln.Addr().String()
	if st := c.check(context.Background(), "proxied.com"); st.Status != "pending" || st.Proxy != proxyCloudflare || !strings.Contains(st.Message, "Always Use HTTPS") {
		t.Errorf("proxied pending = %+v", st)
	}
	if st := c.check(context.Background(), "direct.com"); st.Status != "pending" || st.Proxy != "" || !strings.Contains(st.Message, "Point the domain's DNS") {
		t.Errorf("direct pending = %+v", st)
	}
}

func TestAutoForceHTTPSSkipsCloudflare(t *testing.T) {
	h := newHarness(t)
	admin := h.login("fastcp", "admin-pass")
	r := h.do("POST", "/api/sites", admin, map[string]any{"domain": "example.com"})
	expect(t, r, 201, "create site")
	ctx := context.Background()
	site, err := h.db.GetSite(ctx, r.body["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	domains, err := h.db.ListDomains(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}

	h.srv.ssl.store("example.com", sslStatus{Valid: true, Status: "valid", Proxy: proxyCloudflare})
	h.srv.autoForceHTTPS(ctx, site, domains)
	if cur, _ := h.db.GetSite(ctx, site.ID); cur.ForceHTTPS {
		t.Fatal("HTTPS redirect turned on for a site behind Cloudflare")
	}

	h.srv.ssl.store("example.com", sslStatus{Valid: true, Status: "valid"})
	h.srv.autoForceHTTPS(ctx, site, domains)
	if cur, _ := h.db.GetSite(ctx, site.ID); !cur.ForceHTTPS {
		t.Fatal("HTTPS redirect not turned on once the certificate is valid")
	}
}
