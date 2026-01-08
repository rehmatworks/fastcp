package api

import (
	"os/exec"
	"strings"
)

// runCommand runs a command and returns combined output (stdout+stderr).
// Tests can override this variable to mock system commands.
var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// runCommandWithInput runs a command while providing input on stdin and returns combined output.
// Signature: runCommandWithInput(input, name, args...)
var runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}
