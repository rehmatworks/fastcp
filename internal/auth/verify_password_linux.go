//go:build pam

package auth

import (
	pam "github.com/msteinert/pam"
)

// verifyPassword uses PAM on systems when built with the "pam" tag to authenticate the provided username/password.
// If PAM authentication fails for any reason, it falls back to the shadow-based verifier.
func verifyPassword(username, password string) bool {
	// Use service name "login" which should be present on most Linux systems
	txn, err := pam.StartFunc("login", username, func(style pam.Style, message string) (string, error) {
		switch style {
		case pam.PromptEchoOff, pam.PromptEchoOn:
			return password, nil
		case pam.ErrorMsg, pam.TextInfo:
			return "", nil
		}
		return "", nil
	})
	if err == nil {
		if err = txn.Authenticate(pam.Silent); err == nil {
			// Optionally also authorize
			if err = txn.AcctMgmt(pam.Silent); err == nil {
				return true
			}
		}
	}

	// PAM failed: fallback to shadow-based verification
	return verifyPasswordShadow(username, password)
}
