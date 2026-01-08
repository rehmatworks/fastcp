package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixUserPermissionsForUsers_MockedCommands(t *testing.T) {
	// Prepare temp home directories to avoid touching /home
	goodHome := t.TempDir()
	badHome := t.TempDir()

	// Create a www dir for both users so webDir checks pass
	_ = os.MkdirAll(filepath.Join(goodHome, "www"), 0755)
	_ = os.MkdirAll(filepath.Join(badHome, "www"), 0755)

	// Save original runCommand and restore at end
	origRun := runCommand
	defer func() { runCommand = origRun }()

	// Mock runCommand: fail chown for baduser when operating on badHome
	runCommand = func(name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if name == "chown" && strings.Contains(joined, "baduser") {
			return []byte("chown: failed permission denied"), fmt.Errorf("simulated chown error")
		}
		// Simulate successful chmod/chown otherwise
		return []byte("ok"), nil
	}

	users := []FastCPUser{
		{Username: "gooduser", HomeDir: goodHome},
		{Username: "baduser", HomeDir: badHome},
	}

	s := &Server{}
	details, fixed, errors := s.fixUserPermissionsForUsers(users)

	if fixed != 2 {
		t.Fatalf("expected fixed=2, got %d", fixed)
	}
	if errors < 1 {
		t.Fatalf("expected at least one error, got %d", errors)
	}

	// Validate details contain the error message for baduser home_chown
	badDetails, ok := details["baduser"].(map[string]string)
	if !ok {
		t.Fatalf("expected baduser details to be map[string]string, got %T", details["baduser"])
	}

	homeChown, ok := badDetails["home_chown"]
	if !ok {
		t.Fatalf("expected baduser home_chown detail, details: %v", badDetails)
	}
	if !strings.Contains(homeChown, "simulated chown error") && !strings.Contains(homeChown, "permission denied") {
		t.Fatalf("unexpected home_chown detail: %s", homeChown)
	}
}
