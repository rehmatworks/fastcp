//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureOwnedDirKeepsSetgid(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	for range 2 {
		if err := ensureOwnedDir(dir, 0o750|os.ModeSetgid, os.Getuid(), os.Getgid()); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSetgid == 0 || fi.Mode().Perm() != 0o750 {
			t.Fatalf("mode = %v, want setgid and 0750", fi.Mode())
		}
	}
}
