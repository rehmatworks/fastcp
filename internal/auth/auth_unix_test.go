package auth

import (
	"os"
	"os/user"
	"testing"

	"github.com/rehmatworks/fastcp/internal/config"
)

func TestAuthenticateUnix_AdminRole(t *testing.T) {
	// Ensure we run authenticateUnix directly without relying on real system
	SetPasswordVerifier(func(u, p string) bool { return true })
	SetGroupChecker(func(u, g string) bool {
		if g == "root" {
			return true
		}
		return false
	})
	SetUserLookup(func(username string) (*user.User, error) {
		return &user.User{Uid: "1000", Username: username}, nil
	})
	defer func() { SetPasswordVerifier(nil); SetGroupChecker(nil); SetUserLookup(nil) }()

	user, err := authenticateUnix("alice", "pw")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.Username != "alice" {
		t.Fatalf("expected username alice, got %s", user.Username)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin role, got %s", user.Role)
	}
}

func TestAuthenticateUnix_NotAllowed(t *testing.T) {
	SetPasswordVerifier(func(u, p string) bool { return true })
	SetGroupChecker(func(u, g string) bool { return false })
	SetUserLookup(func(username string) (*user.User, error) {
		return &user.User{Uid: "1001", Username: username}, nil
	})
	defer func() { SetPasswordVerifier(nil); SetGroupChecker(nil); SetUserLookup(nil) }()

	_, err := authenticateUnix("bob", "pw")
	if err == nil {
		t.Fatalf("expected ErrUserNotAllowed, got nil")
	}
	if err != ErrUserNotAllowed {
		t.Fatalf("expected ErrUserNotAllowed, got: %v", err)
	}
}

func TestAuthenticateUnix_InvalidPassword(t *testing.T) {
	SetPasswordVerifier(func(u, p string) bool { return false })
	SetGroupChecker(func(u, g string) bool { return true })
	SetUserLookup(func(username string) (*user.User, error) {
		return &user.User{Uid: "1002", Username: username}, nil
	})
	defer func() { SetPasswordVerifier(nil); SetGroupChecker(nil); SetUserLookup(nil) }()

	_, err := authenticateUnix("carl", "wrong")
	if err == nil {
		t.Fatalf("expected invalid credentials error, got nil")
	}
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

// Sanity check: dev admin still works when FASTCP_DEV=1
func TestDevAdminAuthenticateHook(t *testing.T) {
	os.Setenv("FASTCP_DEV", "1")
	defer os.Unsetenv("FASTCP_DEV")

	cfg, _ := config.Load("")
	cfg.AdminUser = "devadmin"
	cfg.AdminPassword = "pw"
	cfg.AllowAdminPasswordLogin = true
	config.Update(cfg)

	u, err := Authenticate("devadmin", "pw")
	if err != nil {
		t.Fatalf("expected dev admin auth to work, got: %v", err)
	}
	if u.Role != "admin" {
		t.Fatalf("expected admin role for dev admin, got %s", u.Role)
	}
	_ = u
}
