//go:build linux || darwin

package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func setup(t *testing.T) (root *Dir, base, outside string) {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base = filepath.Join(tmp, "home")
	outside = filepath.Join(tmp, "outside")
	for _, d := range []string{base, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err = OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, base, outside
}

func ids() (int, int) { return os.Getuid(), os.Getgid() }

func TestOpenRootRequiresCleanAbsolutePath(t *testing.T) {
	for _, p := range []string{"relative", "/tmp/../etc", "/tmp/"} {
		if _, err := OpenRoot(p); !errors.Is(err, ErrBadPath) {
			t.Errorf("OpenRoot(%q) = %v, want ErrBadPath", p, err)
		}
	}
}

func TestOpenDirRefusesSymlinks(t *testing.T) {
	root, base, outside := setup(t)
	if err := os.Symlink(outside, filepath.Join(base, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.OpenDir(".ssh"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("OpenDir through symlink = %v, want ErrSymlink", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "apps", "site")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.OpenDir("apps/site"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("OpenDir nested symlink = %v, want ErrSymlink", err)
	}
	for _, rel := range []string{"..", "apps/../..", "/etc", "a//b", ""} {
		if _, err := root.OpenDir(rel); !errors.Is(err, ErrBadPath) {
			t.Errorf("OpenDir(%q) = %v, want ErrBadPath", rel, err)
		}
	}
}

func TestMkdirAllRefusesSymlinkedParent(t *testing.T) {
	root, base, outside := setup(t)
	uid, gid := ids()
	if err := os.Symlink(outside, filepath.Join(base, "apps")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.MkdirAll("apps/example/public", 0o750, uid, gid); !errors.Is(err, ErrSymlink) {
		t.Fatalf("MkdirAll through symlink = %v, want ErrSymlink", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "example")); !os.IsNotExist(err) {
		t.Fatal("directory was created outside the root")
	}
}

func TestMkdirSetsExactMode(t *testing.T) {
	root, base, _ := setup(t)
	uid, gid := ids()
	d, err := root.MkdirAll("apps/site/public", 0o750, uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	st, err := os.Stat(filepath.Join(base, "apps/site/public"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v, want 0750", st.Mode().Perm())
	}
	if err := os.Chmod(filepath.Join(base, "apps"), 0o777); err != nil {
		t.Fatal(err)
	}
	d, err = root.EnsureDir("apps", 0o711, uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	st, _ = os.Stat(filepath.Join(base, "apps"))
	if st.Mode().Perm() != 0o711 {
		t.Fatalf("EnsureDir mode = %v, want 0711", st.Mode().Perm())
	}
}

func TestWriteFileReplacesSymlinkInsteadOfFollowing(t *testing.T) {
	root, base, outside := setup(t)
	uid, gid := ids()
	target := filepath.Join(outside, "secret")
	if err := os.Symlink(target, filepath.Join(base, "authorized_keys")); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("authorized_keys", []byte("ssh-ed25519 AAAA\n"), 0o600, uid, gid); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "secret" {
		t.Fatalf("symlink target was modified: %q", b)
	}
	fi, err := os.Lstat(filepath.Join(base, "authorized_keys"))
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Fatalf("expected a regular 0600 file, got %v %v", fi, err)
	}
}

func TestWriteFileDoesNotWriteThroughHardlink(t *testing.T) {
	root, base, outside := setup(t)
	uid, gid := ids()
	target := filepath.Join(outside, "secret")
	if err := os.Link(target, filepath.Join(base, "config")); err != nil {
		t.Skip("hard links not supported:", err)
	}
	if err := root.WriteFile("config", []byte("new"), 0o644, uid, gid); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "secret" {
		t.Fatalf("hard link target was modified: %q", b)
	}
}

func TestReadFileRefusesSymlinksAndLimits(t *testing.T) {
	root, base, outside := setup(t)
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := root.ReadFile("link", 1024); !errors.Is(err, ErrSymlink) {
		t.Fatalf("ReadFile(symlink) = %v, want ErrSymlink", err)
	}
	if err := os.WriteFile(filepath.Join(base, "big"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := root.ReadFile("big", 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadFile(big) = %v, want ErrTooLarge", err)
	}
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := root.ReadFile("dir", 1024); !errors.Is(err, ErrNotFile) {
		t.Fatalf("ReadFile(dir) = %v, want ErrNotFile", err)
	}
}

func TestRemoveAllDoesNotFollowSymlinks(t *testing.T) {
	root, base, outside := setup(t)
	site := filepath.Join(base, "apps", "site")
	if err := os.MkdirAll(filepath.Join(site, "public", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "public", "deep", "index.php"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(site, "public", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(site, "secret-link")); err != nil {
		t.Fatal(err)
	}
	apps, err := root.OpenDir("apps")
	if err != nil {
		t.Fatal(err)
	}
	defer apps.Close()
	if err := apps.RemoveAll("site"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(site); !os.IsNotExist(err) {
		t.Fatal("site directory still exists")
	}
	if b, err := os.ReadFile(filepath.Join(outside, "secret")); err != nil || string(b) != "secret" {
		t.Fatalf("file outside root was affected: %q %v", b, err)
	}
	if err := apps.RemoveAll("missing"); err != nil {
		t.Fatalf("RemoveAll(missing) = %v", err)
	}
}

func TestRemoveAllOnTopLevelSymlinkRemovesOnlyLink(t *testing.T) {
	root, base, outside := setup(t)
	if err := os.Symlink(outside, filepath.Join(base, "apps")); err != nil {
		t.Fatal(err)
	}
	if err := root.RemoveAll("apps"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret")); err != nil {
		t.Fatal("symlink target contents were removed")
	}
}

func TestWalkSkipsSymlinkTargets(t *testing.T) {
	root, base, outside := setup(t)
	if err := os.MkdirAll(filepath.Join(base, "public", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "public", "css", "a.css"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "public", "out")); err != nil {
		t.Fatal(err)
	}
	var seen []string
	err := root.Walk(func(parent *Dir, name string, info Info) error {
		rel, _ := filepath.Rel(base, filepath.Join(parent.Path(), name))
		seen = append(seen, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(seen)
	want := []string{"public", "public/css", "public/css/a.css", "public/out"}
	if len(seen) != len(want) {
		t.Fatalf("walk saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("walk saw %v, want %v", seen, want)
		}
	}
}

func TestLchownDoesNotFollow(t *testing.T) {
	root, base, outside := setup(t)
	uid, gid := ids()
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if err := root.Lchown("link", uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := root.Lchown("../x", uid, gid); !errors.Is(err, ErrBadPath) {
		t.Fatalf("Lchown(../x) = %v", err)
	}
}
