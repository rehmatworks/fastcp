package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"os/user"
	"strings"
	"testing"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

func TestChangePassword_Success(t *testing.T) {
	// Set JWT secret
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)

	// Mock Authenticate to succeed for current password
	auth.SetPasswordVerifier(func(u, p string) bool { return true })
	auth.SetGroupChecker(func(u, g string) bool { return true })
	auth.SetUserLookup(func(username string) (*user.User, error) { return &user.User{Uid: "1000", Username: username}, nil })
	defer func() { auth.SetPasswordVerifier(nil); auth.SetGroupChecker(nil); auth.SetUserLookup(nil) }()
	// Mock chpasswd to succeed
	runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
		if name == "chpasswd" {
			if input == "alice:newpass" {
				return []byte(""), nil
			}
			return []byte(""), nil
		}
		return []byte(""), nil
	}
	defer func() {
		runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
			cmd := exec.Command(name, args...)
			cmd.Stdin = strings.NewReader(input)
			return cmd.CombinedOutput()
		}
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	// Generate token for alice
	user := &models.User{ID: "u1", Username: "alice", Role: "user"}
	token, _ := auth.GenerateToken(user)

	body, _ := json.Marshal(map[string]string{"current_password": "pw", "new_password": "newpass1"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(http.HandlerFunc(s.changePassword))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}
}

func TestAddSSHKeyAndDelete_Success(t *testing.T) {
	// Set JWT secret
	cfg, _ := config.Load("")
	cfg.JWTSecret = "test-secret"
	config.Update(cfg)

	// Mock ssh-keygen to return a fingerprint
	runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
		if name == "ssh-keygen" {
			return []byte("256 SHA256:ABCD comment (TYPE)"), nil
		}
		return []byte(""), nil
	}

	// Mock runCommand to accept file ops and cat return empty initially
	runCommand = func(name string, args ...string) ([]byte, error) {
		if name == "cat" {
			return []byte(""), nil
		}
		return []byte(""), nil
	}
	defer func() {
		runCommand = func(name string, args ...string) ([]byte, error) {
			cmd := exec.Command(name, args...)
			return cmd.CombinedOutput()
		}
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	// Generate token for alice
	user := &models.User{ID: "u1", Username: "alice", Role: "user"}
	token, _ := auth.GenerateToken(user)

	// Add SSH key
	body, _ := json.Marshal(map[string]string{"name": "key1", "public_key": "ssh-ed25519 AAAAB3NzaC1yc2EAAAADAQABAAABAQC"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := middleware.AuthMiddleware(http.HandlerFunc(s.addSSHKey))
	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK adding key, got %d", res.StatusCode)
	}

	// Prepare for delete: mock cat to return the existing key so the handler can find it
	runCommand = func(name string, args ...string) ([]byte, error) {
		if name == "cat" {
			return []byte("ssh-ed25519 AAAAB3NzaC1yc2EAAAADAQABAAABAQC key1"), nil
		}
		return []byte(""), nil
	}

	// Now delete key by fingerprint
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/me/ssh-keys/SHA256%3AABCD", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	// Inject chi route param into request context since we're calling handler directly
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("fingerprint", "SHA256:ABCD")
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx))
	w2 := httptest.NewRecorder()

	handler2 := middleware.AuthMiddleware(http.HandlerFunc(s.deleteSSHKey))
	handler2.ServeHTTP(w2, req2)

	res2 := w2.Result()
	defer res2.Body.Close()

	if res2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK deleting key, got %d", res2.StatusCode)
	}
}
