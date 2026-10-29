package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/database"
)

const sslInterval = 10 * time.Minute

type sslStatus struct {
	Valid     bool       `json:"valid"`
	Status    string     `json:"status"` // valid | pending | unreachable | mismatch | untrusted | expired
	Message   string     `json:"message"`
	Issuer    string     `json:"issuer,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Proxy     string     `json:"proxy,omitempty"` // "cloudflare" when the domain resolves to Cloudflare's proxy
	CheckedAt time.Time  `json:"checked_at"`
}

const proxyCloudflare = "cloudflare"

// cloudflareNets are the networks Cloudflare proxies traffic from, as
// published at https://www.cloudflare.com/ips/.
var cloudflareNets = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func isCloudflare(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range cloudflareNets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// sslChecker asks the local web server for each domain's certificate and
// remembers the result, so page loads never trigger TLS handshakes.
type sslChecker struct {
	mu      sync.Mutex
	results map[string]sslStatus
	addr    string
	roots   *x509.CertPool // nil = system roots
	lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
}

func newSSLChecker() *sslChecker {
	return &sslChecker{
		results: map[string]sslStatus{},
		addr:    "127.0.0.1:443",
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
}

// proxyOf reports the CDN a domain resolves to. A domain counts as proxied
// only when every address it resolves to belongs to the CDN.
func (c *sslChecker) proxyOf(ctx context.Context, domain string) string {
	if c.lookup == nil || isLocalDomain(domain) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := c.lookup(ctx, domain)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	for _, a := range addrs {
		if !isCloudflare(a) {
			return ""
		}
	}
	return proxyCloudflare
}

func (c *sslChecker) get(domain string) (sslStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.results[domain]
	return st, ok
}

func (c *sslChecker) forget(domain string) {
	c.mu.Lock()
	delete(c.results, domain)
	c.mu.Unlock()
}

func (c *sslChecker) check(ctx context.Context, domain string) sslStatus {
	now := time.Now().UTC()
	st := sslStatus{CheckedAt: now, Proxy: c.proxyOf(ctx, domain)}
	// What stops a certificate from being issued depends on where the
	// domain's traffic goes.
	issueHint := " Point the domain's DNS at this server and it is issued automatically."
	if st.Proxy == proxyCloudflare {
		issueHint = " Behind Cloudflare's proxy, Always Use HTTPS can block the Let's Encrypt check: turn it off in Cloudflare, or set this record to DNS only, until the certificate is issued."
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// The chain is verified below against the domain; the handshake
		// itself must not fail so the certificate can be inspected.
		Config: &tls.Config{ServerName: domain, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			st.Status, st.Message = "unreachable", "The web server is not answering on port 443."
		} else {
			st.Status, st.Message = "pending", "No certificate has been issued yet."+issueHint
		}
		c.store(domain, st)
		return st
	}
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	conn.Close()
	if len(certs) == 0 {
		st.Status, st.Message = "pending", "No certificate has been issued yet."+issueHint
		c.store(domain, st)
		return st
	}
	leaf := certs[0]
	exp := leaf.NotAfter.UTC()
	st.ExpiresAt = &exp
	st.Issuer = leaf.Issuer.CommonName
	if len(leaf.Issuer.Organization) > 0 {
		st.Issuer = leaf.Issuer.Organization[0]
	}
	inter := x509.NewCertPool()
	for _, ic := range certs[1:] {
		inter.AddCert(ic)
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: domain, Intermediates: inter, Roots: c.roots, CurrentTime: now})
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case err == nil:
		st.Valid, st.Status, st.Message = true, "valid", "A trusted certificate is installed."
	case errors.As(err, &hostErr):
		st.Status, st.Message = "mismatch", "The certificate served does not cover this domain yet."+issueHint
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		st.Status, st.Message = "expired", "The certificate has expired. Renewal is retried automatically."
	default:
		st.Status, st.Message = "untrusted", "The certificate is not trusted yet."+issueHint
	}
	c.store(domain, st)
	return st
}

func (c *sslChecker) store(domain string, st sslStatus) {
	c.mu.Lock()
	c.results[domain] = st
	c.mu.Unlock()
}

// checkAll checks domains a few at a time.
func (c *sslChecker) checkAll(ctx context.Context, domains []string) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, d := range domains {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			c.check(ctx, d)
		}()
	}
	wg.Wait()
}

func isLocalDomain(d string) bool {
	for _, suffix := range []string{".test", ".local", ".example", ".internal", ".lan", ".home.arpa"} {
		if strings.HasSuffix(d, suffix) {
			return true
		}
	}
	return false
}

// autoForceHTTPS turns on HTTPS redirects for a new site once every domain
// it serves has a trusted certificate, unless the owner chose otherwise.
// Sites behind a CDN are left alone: the redirect belongs at the edge, and
// with Cloudflare's Flexible mode an origin redirect never ends.
func (s *Server) autoForceHTTPS(ctx context.Context, site *database.Site, domains []*database.SiteDomain) {
	if !site.ForceHTTPSAuto || site.ForceHTTPS {
		return
	}
	public := 0
	for _, d := range domains {
		if d.RedirectToPrimary || isLocalDomain(d.Domain) {
			continue
		}
		public++
		if st, ok := s.ssl.get(d.Domain); !ok || !st.Valid || st.Proxy != "" {
			return
		}
	}
	if public == 0 {
		return
	}
	err := s.change(ctx, func() (func(), error) {
		cur, err := s.db.GetSite(ctx, site.ID)
		if err != nil {
			return nil, err
		}
		if !cur.ForceHTTPSAuto || cur.ForceHTTPS {
			return nil, nil
		}
		before := *cur
		cur.ForceHTTPS, cur.ForceHTTPSAuto = true, false
		if err := s.db.UpdateSite(ctx, cur); err != nil {
			return nil, err
		}
		return func() { _ = s.db.UpdateSite(ctx, &before) }, nil
	})
	if err != nil {
		slog.Warn("could not enable HTTPS redirects", "site", site.Domain, "error", err)
		return
	}
	slog.Info("enabled HTTPS redirects after the certificate was issued", "site", site.Domain)
}

func (s *Server) sslLoop(ctx context.Context) {
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.sslPass(ctx)
		timer.Reset(sslInterval)
	}
}

func (s *Server) sslPass(ctx context.Context) {
	sites, err := s.db.ListAllSites(ctx)
	if err != nil {
		return
	}
	bySite, err := s.db.DomainsBySite(ctx)
	if err != nil {
		return
	}
	var names []string
	for _, site := range sites {
		for _, d := range bySite[site.ID] {
			names = append(names, d.Domain)
		}
	}
	s.ssl.checkAll(ctx, names)
	for _, site := range sites {
		s.autoForceHTTPS(ctx, site, bySite[site.ID])
	}
}
