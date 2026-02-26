//go:build linux

package api

import (
	"fmt"

	"github.com/msteinert/pam/v2"
)

// pamAuth authenticates using PAM (Linux only)
func (s *AuthService) pamAuth(username, password string) error {
	t, err := pam.StartFunc("login", username, func(style pam.Style, msg string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		default:
			return "", nil
		}
	})
	if err != nil {
		return fmt.Errorf("PAM start failed: %w", err)
	}
	defer t.End()

	if err := t.Authenticate(0); err != nil {
		return fmt.Errorf("PAM authenticate failed: %w", err)
	}

	if err := t.AcctMgmt(0); err != nil {
		return fmt.Errorf("PAM account check failed: %w", err)
	}

	return nil
}
