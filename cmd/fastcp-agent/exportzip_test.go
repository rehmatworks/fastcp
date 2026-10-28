package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"io"
	"sort"
	"testing"
)

func TestExportZip(t *testing.T) {
	entries, err := parseExportMap(`[
		{"src": "/home/alice/apps/blog", "dst": "websites/blog.example.com"},
		{"src": "home/alice/.fastcp/backups/mysql-dumps/alice_wp.sql.gz", "dst": "databases/alice_wp.sql.gz"},
		{"src": "home/alice/.fastcp/backups/manifest.json", "dst": "manifest.json"}
	]`)
	if err != nil {
		t.Fatal(err)
	}

	var in bytes.Buffer
	tw := tar.NewWriter(&in)
	add := func(name string, typ byte, body string) {
		hdr := &tar.Header{Name: name, Typeflag: typ, Mode: 0o644, Size: int64(len(body))}
		if typ == tar.TypeSymlink {
			hdr.Linkname, hdr.Size = "/etc/shadow", 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			io.WriteString(tw, body)
		}
	}
	add("home/alice/apps/blog/", tar.TypeDir, "")
	add("home/alice/apps/blog/public/index.php", tar.TypeReg, "<?php echo 1;")
	add("home/alice/apps/blog/public/evil", tar.TypeSymlink, "")
	add("home/alice/apps/blog2/public/index.php", tar.TypeReg, "other site")
	add("home/alice/.fastcp/backups/mysql-dumps/alice_wp.sql.gz", tar.TypeReg, "dump")
	add("home/alice/.fastcp/backups/manifest.json", tar.TypeReg, "{}")
	add("home/alice/.bash_history", tar.TypeReg, "secret")
	tw.Close()

	var out bytes.Buffer
	n, _, err := writeExport(&out, &in, entries)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{"databases/alice_wp.sql.gz", "manifest.json", "websites/blog.example.com/public/index.php"}
	if n != 3 || len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("archive = %v (n=%d), want %v", names, n, want)
	}
}

func TestExportMapRejectsEscapes(t *testing.T) {
	for _, m := range []string{
		``, `[]`,
		`[{"src": "home/a", "dst": "../x"}]`,
		`[{"src": "home/a", "dst": "/abs"}]`,
		`[{"src": "/", "dst": "x"}]`,
	} {
		if _, err := parseExportMap(m); err == nil {
			t.Errorf("accepted %s", m)
		}
	}
}
