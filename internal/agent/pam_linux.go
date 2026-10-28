//go:build linux && cgo

package agent

import (
	"errors"
	"os"

	"github.com/msteinert/pam/v2"
)

const pamServiceFile = "/etc/pam.d/fastcp"

// pamAuthenticate checks a password and the account state (expiry, locks)
// through PAM.
func pamAuthenticate(username, password string) error {
	service := "login"
	if _, err := os.Stat(pamServiceFile); err == nil {
		service = "fastcp"
	}
	t, err := pam.StartFunc(service, username, func(style pam.Style, msg string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		}
		return "", nil
	})
	if err != nil {
		return err
	}
	defer t.End()
	if err := t.Authenticate(pam.DisallowNullAuthtok); err != nil {
		return errors.New("authentication failed")
	}
	if err := t.AcctMgmt(pam.DisallowNullAuthtok); err != nil {
		return errors.New("account is not permitted to log in")
	}
	return nil
}
