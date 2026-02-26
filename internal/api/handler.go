package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

type contextKey string

const userContextKey contextKey = "user"

// Handler handles all API requests
type Handler struct {
	db            *database.DB
	agent         *agent.Client
	authService   *AuthService
	siteService   *SiteService
	dbService     *DatabaseService
	sshService    *SSHKeyService
	sysService    *SystemService
	userService   *UserService
	cronService   *CronService
	backupService *BackupService
}

// NewHandler creates a new API handler
func NewHandler(db *database.DB, agentClient *agent.Client, version string) *Handler {
	return &Handler{
		db:            db,
		agent:         agentClient,
		authService:   NewAuthService(db),
		siteService:   NewSiteService(db, agentClient),
		dbService:     NewDatabaseService(db, agentClient),
		sshService:    NewSSHKeyService(db, agentClient),
		sysService:    NewSystemService(agentClient, version),
		userService:   NewUserService(db, agentClient),
		cronService:   NewCronService(db, agentClient),
		backupService: NewBackupService(db, agentClient),
	}
}

// StartBackgroundWorkers starts periodic background workers owned by API services.
func (h *Handler) StartBackgroundWorkers(ctx context.Context) {
	if h.backupService != nil {
		h.backupService.Start(ctx)
	}
}

// AuthMiddleware validates the auth token
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			h.error(w, http.StatusUnauthorized, "authorization required")
			return
		}

		user, err := h.authService.ValidateToken(r.Context(), token)
		if err != nil {
			h.error(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) getUser(r *http.Request) *User {
	user, _ := r.Context().Value(userContextKey).(*User)
	return user
}

// IsAuthenticated checks if the request has a valid session (for phpMyAdmin etc)
func (h *Handler) IsAuthenticated(r *http.Request) bool {
	token := extractToken(r)
	if token == "" {
		return false
	}
	_, err := h.authService.ValidateToken(r.Context(), token)
	return err == nil
}

// Auth handlers
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.authService.Login(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Set session cookie for phpMyAdmin and other browser-based access
	http.SetCookie(w, &http.Cookie{
		Name:     "fastcp_session",
		Value:    resp.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  resp.ExpiresAt,
	})

	h.json(w, http.StatusOK, resp)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		h.authService.Logout(r.Context(), token)
	}

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "fastcp_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
	})

	h.json(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	h.json(w, http.StatusOK, user)
}

func (h *Handler) StartImpersonation(w http.ResponseWriter, r *http.Request) {
	adminUser := h.getUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		h.error(w, http.StatusForbidden, "admin access required")
		return
	}
	if adminUser.IsImpersonating {
		h.error(w, http.StatusBadRequest, "cannot start a nested impersonation session")
		return
	}

	var req ImpersonateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.authService.StartImpersonation(
		r.Context(),
		adminUser.Username,
		&req,
		clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "fastcp_session",
		Value:    resp.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  resp.ExpiresAt,
	})
	h.json(w, http.StatusOK, resp)
}

func (h *Handler) StopImpersonation(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.error(w, http.StatusUnauthorized, "authorization required")
		return
	}
	token := extractToken(r)
	if err := h.authService.StopImpersonation(r.Context(), token, user.Username, clientIP(r), r.UserAgent()); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "fastcp_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
	})
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Impersonation session ended."})
}

// Site handlers
func (h *Handler) ListSites(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

	// Parse pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	search := r.URL.Query().Get("search")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	sites, total, err := h.siteService.ListPaginated(r.Context(), user.Username, page, limit, search)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{
		"data":  sites,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *Handler) CreateSite(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

	if user.Username == "root" {
		h.error(w, http.StatusForbidden, "root user cannot create websites for security reasons. Please create websites using a non-root user account.")
		return
	}

	var req CreateSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username

	site, err := h.siteService.Create(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusCreated, site)
}

func (h *Handler) GetSite(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	site, err := h.siteService.Get(r.Context(), id, user.Username)
	if err != nil {
		h.error(w, http.StatusNotFound, "site not found")
		return
	}
	h.json(w, http.StatusOK, site)
}

func (h *Handler) DeleteSite(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	if err := h.siteService.Delete(r.Context(), id, user.Username); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateSiteSettings(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	var req UpdateSiteSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username

	site, err := h.siteService.UpdateSettings(r.Context(), id, user.Username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, site)
}

// Domain handlers
func (h *Handler) AddDomain(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	siteID := chi.URLParam(r, "id")

	var req AddDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username
	req.SiteID = siteID

	domain, err := h.siteService.AddDomain(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusCreated, domain)
}

func (h *Handler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	domainIDStr := chi.URLParam(r, "domainId")
	var domainID int64
	fmt.Sscanf(domainIDStr, "%d", &domainID)

	var req UpdateDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username
	req.DomainID = domainID

	if err := h.siteService.UpdateDomain(r.Context(), &req); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"message": "domain updated"})
}

func (h *Handler) SetPrimaryDomain(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	domainIDStr := chi.URLParam(r, "domainId")
	var domainID int64
	fmt.Sscanf(domainIDStr, "%d", &domainID)

	req := &SetPrimaryDomainRequest{
		Username: user.Username,
		DomainID: domainID,
	}

	if err := h.siteService.SetPrimaryDomain(r.Context(), req); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"message": "primary domain set"})
}

func (h *Handler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	domainIDStr := chi.URLParam(r, "domainId")
	var domainID int64
	fmt.Sscanf(domainIDStr, "%d", &domainID)

	req := &DeleteDomainRequest{
		Username: user.Username,
		DomainID: domainID,
	}

	if err := h.siteService.DeleteDomain(r.Context(), req); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ValidateSlug(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	valid, message, err := h.siteService.ValidateSlug(r.Context(), user.Username, req.Slug)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, map[string]any{
		"valid":   valid,
		"message": message,
	})
}

func (h *Handler) GenerateSlug(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	slug := GenerateSlug(req.Domain)
	h.json(w, http.StatusOK, map[string]string{"slug": slug})
}

func (h *Handler) ValidateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	valid, message, err := h.siteService.ValidateDomain(r.Context(), req.Domain)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, map[string]any{
		"valid":   valid,
		"message": message,
	})
}

// Database handlers
func (h *Handler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

	// Parse pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	search := r.URL.Query().Get("search")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	databases, total, err := h.dbService.ListPaginated(r.Context(), user.Username, page, limit, search)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{
		"data":  databases,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *Handler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username

	database, err := h.dbService.Create(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusCreated, database)
}

func (h *Handler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	if err := h.dbService.Delete(r.Context(), id, user.Username); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ResetDatabasePassword(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	result, err := h.dbService.ResetPassword(r.Context(), id, user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, result)
}

// PhpMyAdminSignon generates a signed token for phpMyAdmin auto-login
func (h *Handler) PhpMyAdminSignon(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	dbUser, dbPassword, dbName, err := h.dbService.GetCredentials(r.Context(), id, user.Username)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate a signed token with credentials, system username, and expiry
	token, err := generatePMAToken(dbUser, dbPassword, dbName, user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	h.json(w, http.StatusOK, map[string]string{
		"url": "/phpmyadmin/?fastcp_token=" + token,
	})
}

// SSH Key handlers
func (h *Handler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	keys, err := h.sshService.List(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, keys)
}

func (h *Handler) AddSSHKey(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

	var req AddSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username

	key, err := h.sshService.Add(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusCreated, key)
}

func (h *Handler) RemoveSSHKey(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	if err := h.sshService.Remove(r.Context(), id, user.Username); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// System handlers
func (h *Handler) SystemStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.sysService.GetStatus(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, status)
}

func (h *Handler) SystemServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.sysService.GetServices(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, services)
}

func (h *Handler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	force := strings.TrimSpace(r.URL.Query().Get("force"))
	info, err := h.sysService.CheckUpdate(r.Context(), force == "1" || strings.EqualFold(force, "true"))
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, info)
}

func (h *Handler) GetMySQLConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.sysService.GetMySQLConfig(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) SetMySQLConfig(w http.ResponseWriter, r *http.Request) {
	var cfg agent.MySQLConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.sysService.SetMySQLConfig(r.Context(), &cfg); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "MySQL configuration updated and service restarted."})
}

func (h *Handler) GetSSHConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.sysService.GetSSHConfig(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) SetSSHConfig(w http.ResponseWriter, r *http.Request) {
	var cfg agent.SSHConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.sysService.SetSSHConfig(r.Context(), &cfg); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "SSH configuration updated and service reloaded."})
}

func (h *Handler) GetPHPDefaultConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.sysService.GetPHPDefaultConfig(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) SetPHPDefaultConfig(w http.ResponseWriter, r *http.Request) {
	var cfg agent.PHPDefaultConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.sysService.SetPHPDefaultConfig(r.Context(), &cfg); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Default PHP version updated."})
}

func (h *Handler) InstallPHPVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Version) == "" {
		h.error(w, http.StatusBadRequest, "php version is required")
		return
	}
	status, err := h.sysService.InstallPHPVersion(r.Context(), req.Version)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, status)
}

func (h *Handler) GetPHPVersionInstallStatus(w http.ResponseWriter, r *http.Request) {
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version == "" {
		h.error(w, http.StatusBadRequest, "php version is required")
		return
	}
	h.json(w, http.StatusOK, h.sysService.GetPHPInstallStatus(version))
}

func (h *Handler) GetCaddyConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.sysService.GetCaddyConfig(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) SetCaddyConfig(w http.ResponseWriter, r *http.Request) {
	var cfg agent.CaddyConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.sysService.SetCaddyConfig(r.Context(), &cfg); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Caddy performance settings updated and reloaded."})
}

func (h *Handler) GetFirewallStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.sysService.GetFirewallStatus(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, status)
}

func (h *Handler) InstallFirewall(w http.ResponseWriter, r *http.Request) {
	if err := h.sysService.InstallFirewall(r.Context()); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "UFW installed successfully."})
}

func (h *Handler) SetFirewallEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.sysService.SetFirewallEnabled(r.Context(), req.Enabled); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Firewall state updated."})
}

func (h *Handler) FirewallAllowPort(w http.ResponseWriter, r *http.Request) {
	var req agent.FirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.sysService.FirewallAllowPort(r.Context(), &req); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Port allowed."})
}

func (h *Handler) FirewallDenyPort(w http.ResponseWriter, r *http.Request) {
	var req agent.FirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.sysService.FirewallDenyPort(r.Context(), &req); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Port blocked."})
}

func (h *Handler) FirewallDeleteRule(w http.ResponseWriter, r *http.Request) {
	var req agent.FirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.sysService.FirewallDeleteRule(r.Context(), &req); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Firewall rule removed."})
}

func (h *Handler) PerformUpdate(w http.ResponseWriter, r *http.Request) {
	var req PerformUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.sysService.PerformUpdate(r.Context(), req.TargetVersion); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, map[string]string{"status": "ok", "message": "Update initiated. Services will restart shortly."})
}

// AdminMiddleware ensures user is an admin
func (h *Handler) AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := h.getUser(r)
		if user == nil || !user.IsAdmin {
			h.error(w, http.StatusForbidden, "admin access required")
			return
		}
		if user.IsImpersonating {
			h.error(w, http.StatusForbidden, "admin actions are blocked during impersonation")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// User handlers (admin only)
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userService.List(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, users)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.userService.Create(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusCreated, user)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	if err := h.userService.Delete(r.Context(), username); err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ToggleUserSuspension(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	user, err := h.userService.ToggleSuspension(r.Context(), username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, user)
}

func (h *Handler) UpdateUserResources(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	var req UpdateUserResourcesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.userService.UpdateResources(r.Context(), username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusOK, user)
}

// Cron job handlers
func (h *Handler) ListCronJobs(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	jobs, err := h.cronService.List(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, jobs)
}

func (h *Handler) CreateCronJob(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req CreateCronJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username

	job, err := h.cronService.Create(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusCreated, job)
}

func (h *Handler) UpdateCronJob(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req UpdateCronJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = user.Username
	req.ID = chi.URLParam(r, "id")

	job, err := h.cronService.Update(r.Context(), &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, job)
}

func (h *Handler) ToggleCronJob(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.cronService.Toggle(r.Context(), user.Username, id, req.Enabled); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteCronJob(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	id := chi.URLParam(r, "id")

	if err := h.cronService.Delete(r.Context(), user.Username, id); err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ValidateCronExpression(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Expression string `json:"expression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateCronExpression(req.Expression); err != nil {
		h.json(w, http.StatusOK, map[string]any{
			"valid":       false,
			"error":       err.Error(),
			"description": "",
		})
		return
	}

	h.json(w, http.StatusOK, map[string]any{
		"valid":       true,
		"error":       "",
		"description": describeCronExpression(req.Expression),
	})
}

// Helper methods
func (h *Handler) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) error(w http.ResponseWriter, status int, message string) {
	h.json(w, status, map[string]string{"error": message})
}

func extractToken(r *http.Request) string {
	// Check Authorization header first
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Check session cookie (for phpMyAdmin and other browser-based access)
	if cookie, err := r.Cookie("fastcp_session"); err == nil {
		return cookie.Value
	}

	return ""
}

func clientIP(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}
	hostPort := strings.TrimSpace(r.RemoteAddr)
	if hostPort == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return hostPort
}

// generatePMAToken creates an encrypted token for phpMyAdmin signon
func generatePMAToken(dbUser, dbPassword, dbName, sysUsername string) (string, error) {
	expiry := time.Now().Add(5 * time.Minute).Unix()
	payload := fmt.Sprintf("%s|%s|%s|%s|%d", dbUser, dbPassword, dbName, sysUsername, expiry)

	encrypted, err := crypto.EncryptURLSafe(payload)
	if err != nil {
		return "", err
	}

	return encrypted, nil
}
