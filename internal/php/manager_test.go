package php

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rehmatworks/fastcp/internal/caddy"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

func TestStartInstanceNonRoot(t *testing.T) {
	// Ensure dev mode
	os.Setenv("FASTCP_DEV", "1")
	defer os.Unsetenv("FASTCP_DEV")

	cfg, _ := config.Load("")
	cfg.PHPVersions = []models.PHPVersionConfig{{
		Version:    "test",
		Port:       9999,
		AdminPort:  2999,
		BinaryPath: "",
		Enabled:    true,
	}}
	// Create a fake 'franken' script that accepts 'run' and sleeps
	tmpdir := t.TempDir()
	script := filepath.Join(tmpdir, "fakefranken")
	content := "#!/bin/sh\nif [ \"$1\" = \"run\" ]; then sleep 10; else echo ok; fi"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	cfg.PHPVersions[0].BinaryPath = script
	config.Update(cfg)

	mgr := NewManager(caddy.NewGenerator(tmpdir, tmpdir), func() []models.Site { return []models.Site{} })
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// Start instance (should not attempt to setuid when not root)
	if err := mgr.Start("test"); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Give it a moment to start
	time.Sleep(200 * time.Millisecond)

	status := mgr.GetStatus()
	if len(status) != 1 || status[0].Status != "running" {
		t.Fatalf("expected instance running, got: %+v", status)
	}

	// Stop instance and ensure it stops cleanly
	if err := mgr.Stop("test"); err != nil {
		t.Fatalf("failed to stop instance: %v", err)
	}

	status = mgr.GetStatus()
	if len(status) == 1 && status[0].Status == "running" {
		t.Fatalf("expected instance stopped, still running")
	}
}
