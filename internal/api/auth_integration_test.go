package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"log/slog"

	"github.com/rehmatworks/fastcp/internal/config"
)

func TestLoginEndpointWithDevAdmin(t *testing.T) {
	// Enable dev mode and configure dev admin
	os.Setenv("FASTCP_DEV", "1")
	defer os.Unsetenv("FASTCP_DEV")

	cfg, _ := config.Load("")
	cfg.AdminUser = "testadmin"
	cfg.AdminPassword = "secret123"
	cfg.AllowAdminPasswordLogin = true
	config.Update(cfg)

	// Create server with nil managers (login handler doesn't use them)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(nil, nil, nil, nil, nil, nil, nil, logger)

	// Prepare request body
	body, _ := json.Marshal(map[string]string{"username": "testadmin", "password": "secret123"})
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
