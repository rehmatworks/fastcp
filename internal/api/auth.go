package api

import (
	"encoding/json"
	"net/http"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/middleware"
)

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Role     string `json:"role"`
	} `json:"user"`
}

// login handles user authentication
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		s.error(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := auth.Authenticate(req.Username, req.Password)
	if err != nil {
		s.logger.Warn("failed login attempt", "username", req.Username, "error", err)
		s.error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.GenerateToken(user)
	if err != nil {
		s.logger.Error("failed to generate token", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	resp := LoginResponse{
		Token:     token,
		ExpiresIn: 86400, // 24 hours
	}
	resp.User.ID = user.ID
	resp.User.Username = user.Username
	resp.User.Email = user.Email
	resp.User.Role = user.Role

	s.logger.Info("user logged in", "username", user.Username)
	s.success(w, resp)
}

// refreshToken refreshes an authentication token
func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Create a new token with refreshed expiry
	user := &struct {
		ID       string
		Username string
		Role     string
	}{
		ID:       claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
	}

	// For now, just return a success message
	// Full implementation would generate a new token
	s.success(w, map[string]string{
		"message": "token refreshed",
		"user_id": user.ID,
	})
}

// getCurrentUser returns the current authenticated user
func (s *Server) getCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	s.success(w, map[string]interface{}{
		"id":       claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	})
}

// changePassword handles password change requests
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// TODO: Implement password change
	// For now, just return success
	s.success(w, map[string]string{
		"message": "password change not implemented yet",
	})
}

