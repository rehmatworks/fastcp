package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rehmatworks/fastcp/internal/models"
)

// whmcsProvision handles WHMCS provisioning requests
func (s *Server) whmcsProvision(w http.ResponseWriter, r *http.Request) {
	var req models.WHMCSProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.json(w, http.StatusBadRequest, models.WHMCSResponse{
			Result:  "error",
			Message: "invalid request body",
		})
		return
	}

	switch req.Action {
	case "create":
		s.whmcsCreate(w, &req)
	case "suspend":
		s.whmcsSuspend(w, &req)
	case "unsuspend":
		s.whmcsUnsuspend(w, &req)
	case "terminate":
		s.whmcsTerminate(w, &req)
	default:
		s.json(w, http.StatusBadRequest, models.WHMCSResponse{
			Result:  "error",
			Message: "invalid action",
		})
	}
}

// whmcsCreate creates a new account from WHMCS
func (s *Server) whmcsCreate(w http.ResponseWriter, req *models.WHMCSProvisionRequest) {
	if req.Domain == "" {
		s.json(w, http.StatusBadRequest, models.WHMCSResponse{
			Result:  "error",
			Message: "domain is required",
		})
		return
	}

	phpVersion := req.PHPVersion
	if phpVersion == "" {
		phpVersion = "8.4" // Default to latest
	}

	site := &models.Site{
		ID:         uuid.New().String(),
		UserID:     req.Username, // Map to user ID
		Name:       req.Domain,
		Domain:     req.Domain,
		PHPVersion: phpVersion,
		SSL:        true,
		Status:     "active",
	}

	created, err := s.siteManager.Create(site)
	if err != nil {
		s.logger.Error("WHMCS create failed", "error", err, "domain", req.Domain)
		s.json(w, http.StatusInternalServerError, models.WHMCSResponse{
			Result:  "error",
			Message: err.Error(),
		})
		return
	}

	// Reload PHP instances
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances after WHMCS create", "error", err)
	}

	s.logger.Info("WHMCS account created", "service_id", req.ServiceID, "domain", req.Domain)
	s.json(w, http.StatusOK, models.WHMCSResponse{
		Result:  "success",
		Message: "Account created successfully",
		Data: map[string]interface{}{
			"site_id": created.ID,
			"domain":  created.Domain,
		},
	})
}

// whmcsSuspend suspends an account from WHMCS
func (s *Server) whmcsSuspend(w http.ResponseWriter, req *models.WHMCSProvisionRequest) {
	site, err := s.siteManager.GetByDomain(req.Domain)
	if err != nil {
		s.json(w, http.StatusNotFound, models.WHMCSResponse{
			Result:  "error",
			Message: "site not found",
		})
		return
	}

	if err := s.siteManager.Suspend(site.ID); err != nil {
		s.json(w, http.StatusInternalServerError, models.WHMCSResponse{
			Result:  "error",
			Message: err.Error(),
		})
		return
	}

	// Reload PHP instances
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances after WHMCS suspend", "error", err)
	}

	s.logger.Info("WHMCS account suspended", "service_id", req.ServiceID, "domain", req.Domain)
	s.json(w, http.StatusOK, models.WHMCSResponse{
		Result:  "success",
		Message: "Account suspended successfully",
	})
}

// whmcsUnsuspend reactivates a suspended account from WHMCS
func (s *Server) whmcsUnsuspend(w http.ResponseWriter, req *models.WHMCSProvisionRequest) {
	site, err := s.siteManager.GetByDomain(req.Domain)
	if err != nil {
		s.json(w, http.StatusNotFound, models.WHMCSResponse{
			Result:  "error",
			Message: "site not found",
		})
		return
	}

	if err := s.siteManager.Unsuspend(site.ID); err != nil {
		s.json(w, http.StatusInternalServerError, models.WHMCSResponse{
			Result:  "error",
			Message: err.Error(),
		})
		return
	}

	// Reload PHP instances
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances after WHMCS unsuspend", "error", err)
	}

	s.logger.Info("WHMCS account unsuspended", "service_id", req.ServiceID, "domain", req.Domain)
	s.json(w, http.StatusOK, models.WHMCSResponse{
		Result:  "success",
		Message: "Account unsuspended successfully",
	})
}

// whmcsTerminate terminates an account from WHMCS
func (s *Server) whmcsTerminate(w http.ResponseWriter, req *models.WHMCSProvisionRequest) {
	site, err := s.siteManager.GetByDomain(req.Domain)
	if err != nil {
		s.json(w, http.StatusNotFound, models.WHMCSResponse{
			Result:  "error",
			Message: "site not found",
		})
		return
	}

	if err := s.siteManager.Delete(site.ID); err != nil {
		s.json(w, http.StatusInternalServerError, models.WHMCSResponse{
			Result:  "error",
			Message: err.Error(),
		})
		return
	}

	// Reload PHP instances
	if err := s.phpManager.Reload(); err != nil {
		s.logger.Warn("failed to reload PHP instances after WHMCS terminate", "error", err)
	}

	s.logger.Info("WHMCS account terminated", "service_id", req.ServiceID, "domain", req.Domain)
	s.json(w, http.StatusOK, models.WHMCSResponse{
		Result:  "success",
		Message: "Account terminated successfully",
	})
}

// whmcsStatus returns the status of a service
func (s *Server) whmcsStatus(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "service_id")

	// Try to find by domain from query param
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		s.json(w, http.StatusBadRequest, models.WHMCSResponse{
			Result:  "error",
			Message: "domain query parameter is required",
		})
		return
	}

	site, err := s.siteManager.GetByDomain(domain)
	if err != nil {
		s.json(w, http.StatusNotFound, models.WHMCSResponse{
			Result:  "error",
			Message: "site not found",
		})
		return
	}

	s.json(w, http.StatusOK, models.WHMCSResponse{
		Result: "success",
		Data: map[string]interface{}{
			"service_id":  serviceID,
			"site_id":     site.ID,
			"domain":      site.Domain,
			"status":      site.Status,
			"php_version": site.PHPVersion,
			"created_at":  site.CreatedAt,
		},
	})
}

