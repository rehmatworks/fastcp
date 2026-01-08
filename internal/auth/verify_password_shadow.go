package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

// verifyPasswordShadow verifies user's password against the shadow file using Python.
// This is used as a fallback when PAM is unavailable or fails.
func verifyPasswordShadow(username, password string) bool {
	script := `
import crypt
import spwd
try:
    shadow = spwd.getspnam('%s')
    if crypt.crypt('%s', shadow.sp_pwdp) == shadow.sp_pwdp:
        print('OK')
    else:
        print('FAIL')
except:
    print('FAIL')
`

	// Escape single quotes in username and password
	safeUsername := strings.ReplaceAll(username, "'", "\\'")
	safePassword := strings.ReplaceAll(password, "'", "\\'")

	cmd := exec.Command("python3", "-c", fmt.Sprintf(script, safeUsername, safePassword))
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try using su login method
		return verifyPasswordFallbackShadow(username, password)
	}

	return strings.TrimSpace(string(output)) == "OK"
}

// verifyPasswordFallbackShadow uses an alternative method to verify password
// by attempting a simple su invocation (best-effort fallback).
func verifyPasswordFallbackShadow(username, password string) bool {
	script := fmt.Sprintf(`#!/bin/bash
echo '%s' | timeout 5 su -c 'exit 0' %s 2>/dev/null
exit $?
`, strings.ReplaceAll(password, "'", "'\\''"), username)

	cmd := exec.Command("bash", "-c", script)
	err := cmd.Run()

	return err == nil
}
