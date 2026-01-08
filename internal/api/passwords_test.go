package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rehmatworks/fastcp/internal/config"
)

func TestRunChpasswd_Root(t *testing.T) {
	origGetEuid := getEuid
	defer func() { getEuid = origGetEuid }()
	getEuid = func() int { return 0 }

	origRun := runCommandWithInput
	defer func() { runCommandWithInput = origRun }()

	// Mock success
	runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
		if name == "chpasswd" {
			return []byte("ok"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	}

	if _, err := runChpasswd("user1", "pass1"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestRunChpasswd_SudoDisabled(t *testing.T) {
	origGetEuid := getEuid
	defer func() { getEuid = origGetEuid }()
	getEuid = func() int { return 1000 }

	cfg, _ := config.Load("")
	cfg.AllowSudoPasswordChange = false
	config.Update(cfg)

	if _, err := runChpasswd("user2", "pass2"); err == nil {
		t.Fatalf("expected error when sudo fallback disabled")
	}
}

func TestRunChpasswd_SudoRequiresPassword(t *testing.T) {
	origGetEuid := getEuid
	defer func() { getEuid = origGetEuid }()
	getEuid = func() int { return 1000 }

	cfg, _ := config.Load("")
	cfg.AllowSudoPasswordChange = true
	config.Update(cfg)

	origRun := runCommand
	defer func() { runCommand = origRun }()

	// sudo -n true fails
	runCommand = func(name string, args ...string) ([]byte, error) {
		if name == "sudo" {
			return []byte("sudo: a password is required"), errors.New("requires password")
		}
		return []byte("ok"), nil
	}

	if _, err := runChpasswd("user3", "pass3"); err == nil {
		t.Fatalf("expected error when sudo requires password")
	}
}

func TestRunChpasswd_SudoSuccess(t *testing.T) {
	origGetEuid := getEuid
	defer func() { getEuid = origGetEuid }()
	getEuid = func() int { return 1000 }

	cfg, _ := config.Load("")
	cfg.AllowSudoPasswordChange = true
	config.Update(cfg)

	origRun := runCommand
	defer func() { runCommand = origRun }()
	origRunWithInput := runCommandWithInput
	defer func() { runCommandWithInput = origRunWithInput }()

	// sudo -n true succeeds, sudo chpasswd succeeds
	runCommand = func(name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) == 2 && args[0] == "-n" && args[1] == "true" {
			return []byte(""), nil
		}
		return []byte("ok"), nil
	}

	runCommandWithInput = func(input string, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) == 1 && args[0] == "chpasswd" {
			return []byte("ok"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	}

	if _, err := runChpasswd("user4", "pass4"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}
