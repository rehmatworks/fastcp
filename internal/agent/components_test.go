package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCaddyVersion(t *testing.T) {
	cases := map[string]string{
		"v2.11.7 h1:yj0Y4fYZGPkSvibBJ1sTWE33xC0fxztVyXEW5iIdUT4=\n": "2.11.7",
		`{"level":"warn","ts":1793260374.85,"msg":"unable to determine directory for user configuration; falling back to current directory"}` +
			"\nv2.11.7 h1:yj0Y4fYZGPkSvibBJ1sTWE33xC0fxztVyXEW5iIdUT4=\n": "2.11.7",
		"":                 "",
		"(devel)\n":        "",
		"caddy: not found": "",
	}
	for out, want := range cases {
		if got := parseCaddyVersion(out); got != want {
			t.Errorf("parseCaddyVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestNeedsUpgrade(t *testing.T) {
	cases := []struct {
		installed, want string
		upgrade         bool
	}{
		{"", "2.11.7", true},
		{"2.10.2", "2.11.7", true},
		{"2.11.7", "2.11.7", false},
		{"2.12.0", "2.11.7", false},
		{"5.2.2", "5.2.3", true},
		{"0.0.0", "5.2.3", true},
		{"unknown", "2.11.7", false},
		{"(devel)", "2.11.7", false},
	}
	for _, c := range cases {
		if got := needsUpgrade(c.installed, c.want); got != c.upgrade {
			t.Errorf("needsUpgrade(%q, %q) = %v, want %v", c.installed, c.want, got, c.upgrade)
		}
	}
}

func TestPinnedChecksums(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		if len(caddySHA256[arch]) != 64 {
			t.Errorf("caddy %s checksum is not a SHA-256", arch)
		}
	}
	if len(pmaSHA256) != 64 {
		t.Error("phpMyAdmin checksum is not a SHA-256")
	}
}

type entry struct {
	name     string
	typ      byte
	body     string
	mode     int64
	linkname string
}

func writeTarGz(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: mode, Size: int64(len(e.body)), Linkname: e.linkname}
		if e.typ != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractTarGz(t *testing.T) {
	archive := writeTarGz(t, []entry{
		{name: "pma-1.0/", typ: tar.TypeDir},
		{name: "pma-1.0/index.php", typ: tar.TypeReg, body: "<?php"},
		{name: "pma-1.0/bin/tool", typ: tar.TypeReg, body: "#!/bin/sh", mode: 0o4755},
		{name: "pma-1.0/RELEASE-DATE-1.0", typ: tar.TypeReg, body: "x"},
	})
	dest := t.TempDir()
	if err := extractTarGz(archive, dest, 1); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "index.php")); err != nil || string(b) != "<?php" {
		t.Fatalf("index.php = %q, %v", b, err)
	}
	fi, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 || fi.Mode()&os.ModeSetuid != 0 {
		t.Errorf("tool mode = %v, want 0755 without setuid", fi.Mode())
	}
}

func TestExtractTarGzRejectsUnsafeEntries(t *testing.T) {
	for name, entries := range map[string][]entry{
		"symlink":  {{name: "a/link", typ: tar.TypeSymlink, linkname: "/etc/passwd"}},
		"hardlink": {{name: "a/link", typ: tar.TypeLink, linkname: "/etc/passwd"}},
		"device":   {{name: "a/dev", typ: tar.TypeChar}},
	} {
		dest := t.TempDir()
		if err := extractTarGz(writeTarGz(t, entries), dest, 1); err == nil {
			t.Errorf("%s: extraction succeeded", name)
		}
	}

	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := writeTarGz(t, []entry{
		{name: "a/../../escape", typ: tar.TypeReg, body: "x"},
		{name: "/abs", typ: tar.TypeReg, body: "x"},
	})
	if err := extractTarGz(archive, dest, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape")); err == nil {
		t.Error("an entry was written outside the destination")
	}
}

func TestDownloadVerified(t *testing.T) {
	body := []byte("release archive")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	sum := sha256.Sum256(body)
	dir := t.TempDir()

	p, err := downloadVerified(context.Background(), srv.URL, hex.EncodeToString(sum[:]), dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, body) {
		t.Errorf("downloaded %q", b)
	}

	if _, err := downloadVerified(context.Background(), srv.URL, hex.EncodeToString(make([]byte, 32)), dir); err == nil {
		t.Error("a checksum mismatch was accepted")
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 1 {
		t.Errorf("%d files left in the download directory, want 1", len(left))
	}
}
