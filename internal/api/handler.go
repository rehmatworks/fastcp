package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
)

type contextKey string

const userContextKey contextKey = "user"

// Handler handles all API requests
type Handler struct {
	db          *database.DB
	agent       *agent.Client
	authService *AuthService
	siteService *SiteService
	dbService   *DatabaseService
	sshService  *SSHKeyService
	sysService  *SystemService
	userService *UserService
}

// NewHandler creates a new API handler
func NewHandler(db *database.DB, agentClient *agent.Client, version string) *Handler {
	return &Handler{
		db:          db,
		agent:       agentClient,
		authService: NewAuthService(db),
		siteService: NewSiteService(db, agentClient),
		dbService:   NewDatabaseService(db, agentClient),
		sshService:  NewSSHKeyService(db, agentClient),
		sysService:  NewSystemService(agentClient, version),
		userService: NewUserService(db, agentClient),
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

	h.json(w, http.StatusOK, resp)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		h.authService.Logout(r.Context(), token)
	}
	h.json(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	h.json(w, http.StatusOK, user)
}

// Site handlers
func (h *Handler) ListSites(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	sites, err := h.siteService.List(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, sites)
}

func (h *Handler) CreateSite(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)

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

// Database handlers
func (h *Handler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	databases, err := h.dbService.List(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, databases)
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
		h.error(w, http.StatusInternalServerError, err.Error())
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
	info, err := h.sysService.CheckUpdate(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, info)
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
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
