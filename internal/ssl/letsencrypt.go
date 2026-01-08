package ssl

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/models"
)

// ACMEUser implements the registration.User interface
type ACMEUser struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	key          crypto.PrivateKey
}

func (u *ACMEUser) GetEmail() string {
	return u.Email
}

func (u *ACMEUser) GetRegistration() *registration.Resource {
	return u.Registration
}

func (u *ACMEUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

// LetsEncryptProvider defines the ACME provider type
type LetsEncryptProvider string

const (
	ProviderLetsEncrypt LetsEncryptProvider = "letsencrypt"
	ProviderZeroSSL     LetsEncryptProvider = "zerossl"
)

// getACMEDirectory returns the ACME directory URL for the provider
func getACMEDirectory(provider LetsEncryptProvider, staging bool) string {
	if staging {
		switch provider {
		case ProviderZeroSSL:
			return "https://acme.zerossl.com/v2/DV90/directory/staging"
		default:
			return lego.LEDirectoryStaging
		}
	}

	switch provider {
	case ProviderZeroSSL:
		return "https://acme.zerossl.com/v2/DV90"
	default:
		return lego.LEDirectoryProduction
	}
}

// loadOrCreateACMEUser loads or creates an ACME user account
func (m *Manager) loadOrCreateACMEUser(email string) (*ACMEUser, error) {
	accountDir := filepath.Join(m.dataDir, "acme-accounts")
	if err := os.MkdirAll(accountDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create account directory: %w", err)
	}

	accountFile := filepath.Join(accountDir, fmt.Sprintf("%s.json", email))
	keyFile := filepath.Join(accountDir, fmt.Sprintf("%s.key", email))

	// Try to load existing account
	if _, err := os.Stat(accountFile); err == nil {
		data, err := os.ReadFile(accountFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read account file: %w", err)
		}

		var user ACMEUser
		if err := json.Unmarshal(data, &user); err != nil {
			return nil, fmt.Errorf("failed to parse account: %w", err)
		}

		// Load private key
		keyData, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}

		block, _ := pem.Decode(keyData)
		if block == nil {
			return nil, fmt.Errorf("failed to decode private key")
		}

		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}

		user.key = key
		return &user, nil
	}

	// Create new account
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	user := &ACMEUser{
		Email: email,
		key:   privateKey,
	}

	// Save private key
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to save private key: %w", err)
	}

	// Save account info (registration will be updated after registration)
	if err := m.saveACMEUser(user); err != nil {
		return nil, err
	}

	return user, nil
}

// saveACMEUser saves the ACME user account
func (m *Manager) saveACMEUser(user *ACMEUser) error {
	accountDir := filepath.Join(m.dataDir, "acme-accounts")
	accountFile := filepath.Join(accountDir, fmt.Sprintf("%s.json", user.Email))

	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	if err := os.WriteFile(accountFile, data, 0600); err != nil {
		return fmt.Errorf("failed to save account: %w", err)
	}

	return nil
}

// IssueLetsEncryptCertificate obtains a Let's Encrypt certificate for a domain
func (m *Manager) IssueLetsEncryptCertificate(siteID, domain, email string, provider LetsEncryptProvider, staging bool) (*models.SSLCertificate, error) {
	if email == "" {
		return nil, fmt.Errorf("email is required for Let's Encrypt")
	}

	// Load or create ACME user
	user, err := m.loadOrCreateACMEUser(email)
	if err != nil {
		return nil, fmt.Errorf("failed to load ACME user: %w", err)
	}

	// Create lego config
	config := lego.NewConfig(user)
	config.CADirURL = getACMEDirectory(provider, staging)
	config.Certificate.KeyType = certcrypto.EC256

	// Create lego client
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Setup HTTP-01 challenge
	// Note: In production, you would configure the challenge handler to work with your web server
	// For now, we'll use the built-in HTTP server which requires port 80 to be accessible
	err = client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80"))
	if err != nil {
		return nil, fmt.Errorf("failed to setup HTTP-01 challenge: %w", err)
	}

	// Register account if not already registered
	if user.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{
			TermsOfServiceAgreed: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to register ACME account: %w", err)
		}
		user.Registration = reg
		if err := m.saveACMEUser(user); err != nil {
			return nil, fmt.Errorf("failed to save ACME registration: %w", err)
		}
	}

	// Request certificate
	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}

	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain certificate: %w", err)
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
	if err := os.WriteFile(certPath, certificates.Certificate, 0644); err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}

	// Save private key
	if err := os.WriteFile(keyPath, certificates.PrivateKey, 0600); err != nil {
		return nil, fmt.Errorf("failed to save private key: %w", err)
	}

	// Save issuer certificate (chain)
	if err := os.WriteFile(chainPath, certificates.IssuerCertificate, 0644); err != nil {
		return nil, fmt.Errorf("failed to save chain: %w", err)
	}

	// Parse certificate to get validity dates
	block, _ := pem.Decode(certificates.Certificate)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Create certificate record
	providerStr := string(provider)
	sslCert := &models.SSLCertificate{
		ID:         certID,
		SiteID:     siteID,
		Domain:     domain,
		Type:       "letsencrypt",
		Status:     "active",
		Provider:   providerStr,
		AutoRenew:  true,
		Email:      email,
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

// RenewCertificate renews a Let's Encrypt certificate
func (m *Manager) RenewCertificate(certID string) (*models.SSLCertificate, error) {
	cert, err := m.GetCertificate(certID)
	if err != nil {
		return nil, err
	}

	if cert.Type != "letsencrypt" {
		return nil, fmt.Errorf("only Let's Encrypt certificates can be renewed automatically")
	}

	// Check if certificate needs renewal (within 30 days of expiry)
	daysUntilExpiry := time.Until(cert.ValidUntil).Hours() / 24
	if daysUntilExpiry > 30 {
		return cert, nil // No renewal needed
	}

	// For renewal, we need the email used for registration
	email := cert.Email
	if email == "" {
		return nil, fmt.Errorf("certificate email not found - cannot renew")
	}

	provider := ProviderLetsEncrypt
	if cert.Provider == string(ProviderZeroSSL) {
		provider = ProviderZeroSSL
	}

	// Delete old certificate
	if err := m.DeleteCertificate(certID); err != nil {
		return nil, fmt.Errorf("failed to delete old certificate: %w", err)
	}

	// Issue new certificate
	newCert, err := m.IssueLetsEncryptCertificate(cert.SiteID, cert.Domain, email, provider, false)
	if err != nil {
		return nil, fmt.Errorf("failed to renew certificate: %w", err)
	}

	newCert.LastRenewed = time.Now()

	// Update in database
	certs, err := m.loadCertificates()
	if err != nil {
		return nil, err
	}

	certs[newCert.ID] = newCert
	if err := m.saveCertificates(certs); err != nil {
		return nil, err
	}

	return newCert, nil
}

// AutoRenewCertificates checks and renews certificates that are expiring soon
func (m *Manager) AutoRenewCertificates() error {
	expiring, err := m.CheckExpiringSoon(30)
	if err != nil {
		return err
	}

	for _, cert := range expiring {
		if cert.AutoRenew && cert.Type == "letsencrypt" {
			if _, err := m.RenewCertificate(cert.ID); err != nil {
				// Log error but continue with other certificates
				fmt.Printf("Failed to renew certificate %s (%s): %v\n", cert.ID, cert.Domain, err)

				// Update status to failed
				m.UpdateCertificateStatus(cert.ID, "failed")
			}
		}
	}

	return nil
}
