package ownerfs

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

// site creates page.txt, sub/inner.txt, a link to each and a link to sub.
func site(t *testing.T) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "page.txt"), []byte("hello"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "inner.txt"), []byte("inner"), 0o644)
	os.Symlink(filepath.Join(dir, "page.txt"), filepath.Join(dir, "link.txt"))
	os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "linkdir"))
	return dir
}

func served(f *FS, name string) error {
	if _, err := f.Stat(name); err != nil {
		return err
	}
	file, err := f.Open(name)
	if err != nil {
		return err
	}
	return file.Close()
}

func refused(t *testing.T, f *FS, name string) {
	t.Helper()
	if _, err := f.Stat(name); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s) = %v, want not exist", name, err)
	}
	if file, err := f.Open(name); !errors.Is(err, fs.ErrNotExist) {
		if file != nil {
			file.Close()
		}
		t.Errorf("Open(%s) = %v, want not exist", name, err)
	}
}

func TestServesTheOwnersFiles(t *testing.T) {
	dir := site(t)
	f := &FS{UID: os.Getuid()}
	for _, name := range []string{"page.txt", "link.txt", "sub", "sub/inner.txt", "linkdir", "linkdir/inner.txt"} {
		if err := served(f, filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	file, _ := f.Open(filepath.Join(dir, "linkdir/inner.txt"))
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(data) != "inner" {
		t.Fatalf("read %q, %v", data, err)
	}
	refused(t, f, filepath.Join(dir, "missing"))
}

func TestLinksNeedTheOwner(t *testing.T) {
	dir := site(t)
	other := &FS{UID: os.Getuid() + 1}
	for _, name := range []string{"link.txt", "linkdir", "linkdir/inner.txt"} {
		refused(t, other, filepath.Join(dir, name))
	}
	// Without links, the owner does not matter where openat2 can tell.
	for _, name := range []string{"page.txt", "sub/inner.txt"} {
		err := served(other, filepath.Join(dir, name))
		if runtime.GOOS == "linux" && err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if runtime.GOOS != "linux" && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, want not exist", name, err)
		}
	}

	if os.Getuid() != 0 {
		link := filepath.Join(dir, "hosts.txt")
		os.Symlink("/etc/hosts", link)
		refused(t, &FS{UID: os.Getuid()}, link)
	}
}

func TestFIFODoesNotBlock(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	f := &FS{UID: os.Getuid()}
	done := make(chan struct{})
	go func() {
		refused(t, f, fifo)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocked on a FIFO")
	}
}

func TestUnmarshalCaddyfile(t *testing.T) {
	for input, want := range map[string]int{
		"fastcp_owner 1001":           1001,
		"fastcp_owner 4294967294":     4294967294,
		"fastcp_owner":                -1,
		"fastcp_owner 0":              -1,
		"fastcp_owner x":              -1,
		"fastcp_owner -5":             -1,
		"fastcp_owner 4294967295":     -1,
		"fastcp_owner 99999999999999": -1,
		"fastcp_owner 1001 1002":      -1,
		"fastcp_owner 1001 {\nx\n}":   -1,
	} {
		var f FS
		err := f.UnmarshalCaddyfile(caddyfile.NewTestDispenser(input))
		if want < 0 {
			if err == nil {
				t.Errorf("%q accepted", input)
			}
			continue
		}
		if err != nil || f.UID != want {
			t.Errorf("%q = %d, %v", input, f.UID, err)
		}
	}
}

func TestCaddyfileWiring(t *testing.T) {
	adapter := caddyconfig.GetAdapter("caddyfile")
	out, _, err := adapter.Adapt([]byte(`{
	filesystem alice fastcp_owner 1001
}
http://example.com {
	root * /home/alice/apps/example/public
	fs alice
	file_server
}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Apps struct {
			Filesystems struct {
				Filesystems []struct {
					Name       string          `json:"name"`
					FileSystem json.RawMessage `json:"file_system"`
				} `json:"filesystems"`
			} `json:"caddy.filesystems"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	fss := cfg.Apps.Filesystems.Filesystems
	if len(fss) != 1 || fss[0].Name != "alice" || string(fss[0].FileSystem) != `{"backend":"fastcp_owner","uid":1001}` {
		t.Fatalf("filesystems = %s", out)
	}
	if !strings.Contains(string(out), `"fs":"alice"`) {
		t.Fatalf("site does not use the file system: %s", out)
	}
}
