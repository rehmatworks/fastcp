package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/rehmatworks/fastcp/internal/models"
	"github.com/rehmatworks/fastcp/internal/sites"
)

type fakeSiteManager struct{}

func (f *fakeSiteManager) List(uid string) []models.Site           { return []models.Site{} }
func (f *fakeSiteManager) SetUserLimit(l *models.UserLimits) error { return nil }
func (f *fakeSiteManager) GetUserLimit(username string) *models.UserLimits {
	return &models.UserLimits{}
}

func TestInspectFixPermissions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Create a real sites.Manager backed by a temporary data path so Server initialization succeeds
	dataPath := t.TempDir()
	siteMgr := sites.NewManager(dataPath)
	_ = siteMgr.Load()
	s := NewServer(siteMgr, nil, nil, nil, nil, nil, nil, logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users/fix-permissions", nil)

	// Call handler directly (bypassing auth for inspection)
	s.fixUserPermissions(w, req)

	res := w.Result()
	defer res.Body.Close()

	var out map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Log detailed results for inspection
	if details, ok := out["details"].(map[string]interface{}); ok {
		for user, v := range details {
			t.Logf("user: %s -> %v", user, v)
		}
	}

	if out["errors"] == nil {
		t.Fatalf("expected errors field in response, got: %v", out)
	}

	t.Logf("fix-permissions result: %v", out)
}
