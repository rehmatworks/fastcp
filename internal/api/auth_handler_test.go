package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"testing"

	"log/slog"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

func TestLogin_Success_UnixAuth(t *testing.T) {
	// Ensure dev mode is off
	os.Unsetenv("FASTCP_DEV")
	// Ensure config is initialized and set JWT secret
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)
	// Mock system interactions
	auth.SetPasswordVerifier(func(u, p string) bool { return true })
	auth.SetGroupChecker(func(u, g string) bool { return true })
	auth.SetUserLookup(func(username string) (*user.User, error) {
		return &user.User{Uid: "1000", Username: username}, nil
	})
	defer func() { auth.SetPasswordVerifier(nil); auth.SetGroupChecker(nil); auth.SetUserLookup(nil) }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "pw"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.login(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := data["token"]; !ok {
		t.Fatalf("expected token in response, got: %v", data)
	}
}

func TestLogin_Fail_InvalidCreds(t *testing.T) {
	// Ensure dev mode is off
	os.Unsetenv("FASTCP_DEV")
	// Ensure config is initialized and set JWT secret
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)
	// Mock system interactions: invalid password
	auth.SetPasswordVerifier(func(u, p string) bool { return false })
	auth.SetGroupChecker(func(u, g string) bool { return true })
	auth.SetUserLookup(func(username string) (*user.User, error) {
		return &user.User{Uid: "1001", Username: username}, nil
	})
	defer func() { auth.SetPasswordVerifier(nil); auth.SetGroupChecker(nil); auth.SetUserLookup(nil) }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	body, _ := json.Marshal(map[string]string{"username": "bob", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.login(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", res.StatusCode)
	}
}

func TestRefreshToken_Success(t *testing.T) {
	// Create a token for a user
	user := &models.User{ID: "u1", Username: "alice", Role: "user"}
	// Ensure JWT secret is set via config
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	// Wrap with AuthMiddleware so claims are set
	handler := middleware.AuthMiddleware(http.HandlerFunc(s.refreshToken))
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

	if _, ok := data["token"]; !ok {
		t.Fatalf("expected token in response, got: %v", data)
	}
}
