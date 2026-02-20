//go:build darwin

package api

import (
	"fmt"
	"os/user"
)

// pamAuth on macOS - simplified for development
// In dev mode, accept any password for existing system users
func (s *AuthService) pamAuth(username, password string) error {
	// Check if user exists on the system
	_, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// For development on macOS, we skip actual password verification
	// In production on Linux, PAM handles this properly
	if password == "" {
		return fmt.Errorf("password required")
	}

	// WARNING: This is insecure and only for local development!
	// The Linux version uses proper PAM authentication
	return nil
}
