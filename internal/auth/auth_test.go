package auth

import (
	"os"
	"testing"

	"github.com/rehmatworks/fastcp/internal/config"
)

func TestDevAdminAuthenticate(t *testing.T) {
	// Enable dev mode
	os.Setenv("FASTCP_DEV", "1")
	defer os.Unsetenv("FASTCP_DEV")

	// Ensure config is initialized and set admin creds
	cfg, _ := config.Load("")
	cfg.AdminUser = "testadmin"
	cfg.AdminPassword = "secret123"
	cfg.AllowAdminPasswordLogin = true
	config.Update(cfg)

	user, err := Authenticate("testadmin", "secret123")
	if err != nil {
		t.Fatalf("expected successful auth, got error: %v", err)
	}
	if user.Username != "testadmin" || user.Role != "admin" {
		t.Fatalf("unexpected user returned: %+v", user)
	}
}

func TestDevAdminAuthenticateDisabled(t *testing.T) {
	// Ensure dev mode is off
	os.Unsetenv("FASTCP_DEV")

	cfg, _ := config.Load("")
	cfg.AdminUser = "testadmin"
	cfg.AdminPassword = "secret123"
	config.Update(cfg)

	_, err := Authenticate("testadmin", "secret123")
	if err == nil {
		t.Fatalf("expected authentication to fail when not in dev mode")
	}
}
