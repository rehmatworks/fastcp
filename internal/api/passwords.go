package api

import (
	"fmt"
	"os"
	"strings"

	"github.com/rehmatworks/fastcp/internal/config"
)

// getEuid is a variable so tests can override it. Default returns the real EUID.
var getEuid = func() int { return os.Geteuid() }

// runChpasswd tries to change a user's password. It prefers to run `chpasswd`
// directly when running as root. If not root and the config flag
// AllowSudoPasswordChange is enabled, it will attempt to run
// `sudo -n true` (non-interactive check) and then `sudo chpasswd`.
// Returns combined output and error from the underlying command.
func runChpasswd(username, password string) ([]byte, error) {
	input := fmt.Sprintf("%s:%s", username, password)
	// If running as root, just run chpasswd
	if getEuid() == 0 {
		return runCommandWithInput(input, "chpasswd")
	}

	cfg := config.Get()
	if cfg == nil || !cfg.AllowSudoPasswordChange {
		return nil, fmt.Errorf("cannot set password: server not running as root")
	}

	// Verify sudo is available non-interactively
	if _, err := runCommand("sudo", "-n", "true"); err != nil {
		return nil, fmt.Errorf("sudo not available or requires password")
	}

	// Run sudo chpasswd
	out, err := runCommandWithInput(input, "sudo", "chpasswd")
	if err != nil {
		// Include trimmed output for diagnostics
		return out, fmt.Errorf("sudo chpasswd failed: %v", strings.TrimSpace(string(out)))
	}
	return out, nil
}
