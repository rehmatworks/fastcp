package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/models"
)

const (
	certFileName = "server.crt"
	keyFileName  = "server.key"
)

// Manager handles SSL certificate generation and management
type Manager struct {
	dataDir     string
	sslDir      string
	certsDir    string
	certsDBFile string
}

// NewManager creates a new SSL manager
func NewManager(dataDir string) *Manager {
	sslDir := filepath.Join(dataDir, "ssl")
	certsDir := filepath.Join(dataDir, "certificates")
	certsDBFile := filepath.Join(dataDir, "certificates.json")

	// Ensure directories exist
	os.MkdirAll(sslDir, 0700)
	os.MkdirAll(certsDir, 0700)

	return &Manager{
		dataDir:     dataDir,
		sslDir:      sslDir,
		certsDir:    certsDir,
		certsDBFile: certsDBFile,
	}
}

// CertPaths returns the paths to the certificate and key files
func (m *Manager) CertPaths() (certPath, keyPath string) {
	return filepath.Join(m.sslDir, certFileName), filepath.Join(m.sslDir, keyFileName)
}

// CertExists checks if the SSL certificate already exists
func (m *Manager) CertExists() bool {
	certPath, keyPath := m.CertPaths()
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	return certErr == nil && keyErr == nil
}

// EnsureCertificate ensures a valid SSL certificate exists, generating one if necessary
func (m *Manager) EnsureCertificate() error {
	if m.CertExists() {
		return nil
	}
	return m.GenerateCertificate()
}

// GenerateCertificate generates a new self-signed SSL certificate
func (m *Manager) GenerateCertificate() error {
	// Ensure SSL directory exists
	if err := os.MkdirAll(m.sslDir, 0700); err != nil {
		return fmt.Errorf("failed to create SSL directory: %w", err)
	}

	// Generate ECDSA private key (P-256 curve)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate a random serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Get local IP addresses for SANs
	ipAddresses := getLocalIPs()

	// Certificate template
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:       []string{"FastCP"},
			OrganizationalUnit: []string{"Control Panel"},
			CommonName:         "FastCP Admin Panel",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,

		// Subject Alternative Names
		DNSNames:    []string{"localhost", "fastcp.local"},
		IPAddresses: ipAddresses,
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Save certificate
	certPath, keyPath := m.CertPaths()

	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Save private key
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// getLocalIPs returns all local IP addresses for use in certificate SANs
func getLocalIPs() []net.IP {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Only add non-loopback IPs
			if ip != nil && !ip.IsLoopback() {
				ips = append(ips, ip)
			}
		}
	}

	return ips
}

// RegenerateCertificate forces regeneration of the SSL certificate
func (m *Manager) RegenerateCertificate() error {
	certPath, keyPath := m.CertPaths()

	// Remove existing files
	os.Remove(certPath)
	os.Remove(keyPath)

	return m.GenerateCertificate()
}

// ====================
// Certificate Management
// ====================

// loadCertificates loads all certificates from the database file
func (m *Manager) loadCertificates() (map[string]*models.SSLCertificate, error) {
	certs := make(map[string]*models.SSLCertificate)

	data, err := os.ReadFile(m.certsDBFile)
	if err != nil {
		if os.IsNotExist(err) {
			return certs, nil
		}
		return nil, fmt.Errorf("failed to read certificates database: %w", err)
	}

	if err := json.Unmarshal(data, &certs); err != nil {
		return nil, fmt.Errorf("failed to parse certificates database: %w", err)
	}

	return certs, nil
}

// saveCertificates saves certificates to the database file
func (m *Manager) saveCertificates(certs map[string]*models.SSLCertificate) error {
	data, err := json.MarshalIndent(certs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal certificates: %w", err)
	}

	if err := os.WriteFile(m.certsDBFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write certificates database: %w", err)
	}

	return nil
}

// ListCertificates returns all SSL certificates
func (m *Manager) ListCertificates() ([]*models.SSLCertificate, error) {
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	list := make([]*models.SSLCertificate, 0, len(certs))
	for _, cert := range certs {
		list = append(list, cert)
	}

	return list, nil
}

// GetCertificate retrieves a certificate by ID
func (m *Manager) GetCertificate(id string) (*models.SSLCertificate, error) {
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	cert, ok := certs[id]
	if !ok {
		return nil, fmt.Errorf("certificate not found")
	}

	return cert, nil
}

// GetCertificateBySite retrieves certificates for a specific site
func (m *Manager) GetCertificateBySite(siteID string) ([]*models.SSLCertificate, error) {
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	list := make([]*models.SSLCertificate, 0)
	for _, cert := range certs {
		if cert.SiteID == siteID {
			list = append(list, cert)
		}
	}

	return list, nil
}

// GetCertificateByDomain retrieves a certificate by domain
func (m *Manager) GetCertificateByDomain(domain string) (*models.SSLCertificate, error) {
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, cert := range certs {
		if strings.ToLower(cert.Domain) == domain {
			return cert, nil
		}
	}

	return nil, fmt.Errorf("certificate not found for domain: %s", domain)
}

// IssueSelfSignedCertificate creates a self-signed certificate for a domain
func (m *Manager) IssueSelfSignedCertificate(siteID, domain string) (*models.SSLCertificate, error) {
	// Generate certificate ID and paths
	certID := uuid.New().String()
	certDir := filepath.Join(m.certsDir, certID)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	// Generate ECDSA private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Certificate template
	notBefore := time.Now()
	notAfter := notBefore.AddDate(1, 0, 0) // Valid for 1 year

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"FastCP"},
			CommonName:   domain,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain, "*." + domain},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Save certificate
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return nil, fmt.Errorf("failed to write certificate: %w", err)
	}

	// Save private key
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	// Create certificate record
	cert := &models.SSLCertificate{
		ID:         certID,
		SiteID:     siteID,
		Domain:     domain,
		Type:       "self-signed",
		Status:     "active",
		AutoRenew:  false,
		CertPath:   certPath,
		KeyPath:    keyPath,
		Issuer:     "FastCP Self-Signed",
		Subject:    domain,
		ValidFrom:  notBefore,
		ValidUntil: notAfter,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Save to database
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	certs[certID] = cert
	if err := m.saveCertificates(certs); err != nil {
		return nil, err
	}

	return cert, nil
}

// InstallCustomCertificate installs a custom SSL certificate
func (m *Manager) InstallCustomCertificate(siteID, domain, certPEM, keyPEM, caPEM string) (*models.SSLCertificate, error) {
	// Validate certificate and key
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to parse private key PEM")
	}

	// Verify domain matches certificate
	domainMatch := false
	for _, name := range cert.DNSNames {
		if strings.EqualFold(name, domain) || strings.EqualFold(name, "*."+domain) {
			domainMatch = true
			break
		}
	}
	if !domainMatch && !strings.EqualFold(cert.Subject.CommonName, domain) {
		return nil, fmt.Errorf("certificate does not match domain: %s", domain)
	}

	// Generate certificate ID and paths
	certID := uuid.New().String()
	certDir := filepath.Join(m.certsDir, certID)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")
	chainPath := filepath.Join(certDir, "chain.pem")

	// Save certificate
	if err := os.WriteFile(certPath, []byte(certPEM), 0644); err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}

	// Save private key
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0600); err != nil {
		return nil, fmt.Errorf("failed to save private key: %w", err)
	}

	// Save CA chain if provided
	if caPEM != "" {
		if err := os.WriteFile(chainPath, []byte(caPEM), 0644); err != nil {
			return nil, fmt.Errorf("failed to save CA chain: %w", err)
		}
	}

	// Create certificate record
	sslCert := &models.SSLCertificate{
		ID:         certID,
		SiteID:     siteID,
		Domain:     domain,
		Type:       "custom",
		Status:     "active",
		AutoRenew:  false,
		CertPath:   certPath,
		KeyPath:    keyPath,
		ChainPath:  chainPath,
		Issuer:     cert.Issuer.CommonName,
		Subject:    cert.Subject.CommonName,
		ValidFrom:  cert.NotBefore,
		ValidUntil: cert.NotAfter,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Save to database
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	certs[certID] = sslCert
	if err := m.saveCertificates(certs); err != nil {
		return nil, err
	}

	return sslCert, nil
}

// DeleteCertificate removes a certificate
func (m *Manager) DeleteCertificate(id string) error {
	certs, err := m.loadCertificates()
	if err != nil {
		return err
	}

	cert, ok := certs[id]
	if !ok {
		return fmt.Errorf("certificate not found")
	}

	// Remove certificate files
	certDir := filepath.Dir(cert.CertPath)
	os.RemoveAll(certDir)

	// Remove from database
	delete(certs, id)
	return m.saveCertificates(certs)
}

// CheckExpiringSoon returns certificates expiring within the specified number of days
func (m *Manager) CheckExpiringSoon(days int) ([]*models.SSLCertificate, error) {
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	expiring := make([]*models.SSLCertificate, 0)
	threshold := time.Now().AddDate(0, 0, days)

	for _, cert := range certs {
		if cert.ValidUntil.Before(threshold) && cert.Status == "active" {
			expiring = append(expiring, cert)
		}
	}

	return expiring, nil
}

// UpdateCertificateStatus updates the status of a certificate
func (m *Manager) UpdateCertificateStatus(id, status string) error {
	certs, err := m.loadCertificates()
	if err != nil {
		return err
	}

	cert, ok := certs[id]
	if !ok {
		return fmt.Errorf("certificate not found")
	}

	cert.Status = status
	cert.UpdatedAt = time.Now()

	return m.saveCertificates(certs)
}
