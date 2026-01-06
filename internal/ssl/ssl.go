package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	certFileName = "server.crt"
	keyFileName  = "server.key"
)

// Manager handles SSL certificate generation and management
type Manager struct {
	dataDir string
	sslDir  string
}

// NewManager creates a new SSL manager
func NewManager(dataDir string) *Manager {
	sslDir := filepath.Join(dataDir, "ssl")
	return &Manager{
		dataDir: dataDir,
		sslDir:  sslDir,
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

