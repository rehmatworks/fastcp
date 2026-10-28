//go:build linux

package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSetUserACLOnRealFiles(t *testing.T) {
	root, base, _ := setup(t)
	uid, gid := ids()
	pub, err := root.MkdirAll("public", 0o750, uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	if err := pub.SetUserACL(4242, ACLRead|ACLExec, int(ACLRead|ACLExec)); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) {
			t.Skip("filesystem has no POSIX ACL support")
		}
		t.Fatal(err)
	}
	fd, err := unix.Open(filepath.Join(base, "public"), unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	acl, ok, err := fgetACL(fd, aclXattrAccess)
	if err != nil || !ok {
		t.Fatalf("access ACL missing: %v", err)
	}
	if p, ok := userPerm(acl, 4242); !ok || p != 5 {
		t.Fatalf("access entry = %d %v", p, ok)
	}
	dacl, ok, err := fgetACL(fd, aclXattrDefault)
	if err != nil || !ok {
		t.Fatalf("default ACL missing: %v", err)
	}
	if p, ok := userPerm(dacl, 4242); !ok || p != 5 {
		t.Fatalf("default entry = %d %v", p, ok)
	}

	if err := os.WriteFile(filepath.Join(base, "public", "wp-config.php"), []byte("<?php"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := pub.SetUserACLAt("wp-config.php", 4242, 0, -1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(base, "public", "link")); err != nil {
		t.Fatal(err)
	}
	if err := pub.SetUserACLAt("link", 4242, ACLRead, -1); err != nil {
		t.Fatalf("symlinks must be skipped silently, got %v", err)
	}
}
