package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

func TestGetCurrentUser_Success(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	user := &models.User{ID: "u1", Username: "alice", Role: "user"}
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(http.HandlerFunc(s.getCurrentUser))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if data["username"] != "alice" {
		t.Fatalf("expected username alice, got %v", data["username"])
	}
}

func TestGetCurrentUser_Unauthorized(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(http.HandlerFunc(s.getCurrentUser))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", res.StatusCode)
	}
}

func TestGetConfig_AdminAccess(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	admin := &models.User{ID: "u1", Username: "root", Role: "admin"}
	token, _ := auth.GenerateToken(admin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(middleware.AdminOnlyMiddleware(http.HandlerFunc(s.getConfig)))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for admin, got %d", res.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := data["data_dir"]; !ok {
		t.Fatalf("expected data_dir in config response, got: %v", data)
	}
}

func TestGetConfig_ForbiddenForUser(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	user := &models.User{ID: "u1", Username: "alice", Role: "user"}
	token, _ := auth.GenerateToken(user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(middleware.AdminOnlyMiddleware(http.HandlerFunc(s.getConfig)))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin, got %d", res.StatusCode)
	}
}
