package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
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
	CheckedAt time.Time  `json:"checked_at"`
}

// sslChecker asks the local web server for each domain's certificate and
// remembers the result, so page loads never trigger TLS handshakes.
type sslChecker struct {
	mu      sync.Mutex
	results map[string]sslStatus
	addr    string
	roots   *x509.CertPool // nil = system roots
}

func newSSLChecker() *sslChecker {
	return &sslChecker{results: map[string]sslStatus{}, addr: "127.0.0.1:443"}
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
	st := sslStatus{CheckedAt: now}
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
			st.Status, st.Message = "pending", "No certificate has been issued yet. Point the domain's DNS at this server and it is issued automatically."
		}
		c.store(domain, st)
		return st
	}
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	conn.Close()
	if len(certs) == 0 {
		st.Status, st.Message = "pending", "No certificate has been issued yet."
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
		st.Status, st.Message = "mismatch", "The certificate served does not cover this domain yet."
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		st.Status, st.Message = "expired", "The certificate has expired. Renewal is retried automatically."
	default:
		st.Status, st.Message = "untrusted", "The certificate is not trusted yet. Point the domain's DNS at this server."
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
		if st, ok := s.ssl.get(d.Domain); !ok || !st.Valid {
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
