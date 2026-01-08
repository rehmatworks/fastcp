package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/models"
	"github.com/rehmatworks/fastcp/internal/ssl"
)

// listCertificates returns all SSL certificates
func (s *Server) listCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := s.sslManager.ListCertificates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"certificates": certs,
	})
}

// getCertificate returns a specific certificate
func (s *Server) getCertificate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	cert, err := s.sslManager.GetCertificate(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cert)
}

// getSiteCertificates returns certificates for a specific site
func (s *Server) getSiteCertificates(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "siteId")

	certs, err := s.sslManager.GetCertificateBySite(siteID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"certificates": certs,
	})
}

// issueCertificate issues a new SSL certificate
func (s *Server) issueCertificate(w http.ResponseWriter, r *http.Request) {
	var req models.SSLCertificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.SiteID == "" || req.Domain == "" || req.Type == "" {
		http.Error(w, "site_id, domain, and type are required", http.StatusBadRequest)
		return
	}

	var cert *models.SSLCertificate
	var err error

	switch req.Type {
	case "letsencrypt":
		if req.Email == "" {
			http.Error(w, "email is required for Let's Encrypt certificates", http.StatusBadRequest)
			return
		}

		provider := ssl.ProviderLetsEncrypt
		if req.Provider == "zerossl" {
			provider = ssl.ProviderZeroSSL
		}

		// Use staging for testing (set to false in production)
		staging := false
		cert, err = s.sslManager.IssueLetsEncryptCertificate(req.SiteID, req.Domain, req.Email, provider, staging)

	case "custom":
		if req.CustomCert == "" || req.CustomKey == "" {
			http.Error(w, "custom_cert and custom_key are required for custom certificates", http.StatusBadRequest)
			return
		}

		cert, err = s.sslManager.InstallCustomCertificate(req.SiteID, req.Domain, req.CustomCert, req.CustomKey, req.CustomCA)

	case "self-signed":
		cert, err = s.sslManager.IssueSelfSignedCertificate(req.SiteID, req.Domain)

	default:
		http.Error(w, "invalid certificate type: must be letsencrypt, custom, or self-signed", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(cert)
}

// deleteCertificate removes a certificate
func (s *Server) deleteCertificate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.sslManager.DeleteCertificate(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// renewCertificate renews a Let's Encrypt certificate
func (s *Server) renewCertificate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	cert, err := s.sslManager.RenewCertificate(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cert)
}
