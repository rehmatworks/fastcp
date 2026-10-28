package main

import (
	"archive/tar"
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// exportEntry maps a directory or file inside a snapshot to a path in the
// zip archive.
type exportEntry struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

// exportZip reads a tar stream (restic dump) from stdin and writes the
// mapped entries to a zip file. It runs as the tenant who owns the
// snapshot, never as root, so it can only read what the tenant can.
func exportZip(args []string) error {
	fs := flag.NewFlagSet("export-zip", flag.ContinueOnError)
	out := fs.String("out", "", "zip file to create")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return errors.New("export-zip must not run as root")
	}
	if *out == "" || !filepath.IsAbs(*out) || filepath.Ext(*out) != ".zip" {
		return errors.New("--out must be an absolute path ending in .zip")
	}
	entries, err := parseExportMap(os.Getenv("FASTCP_EXPORT_MAP"))
	if err != nil {
		return err
	}

	partial := *out + ".partial"
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(partial)
		}
	}()
	// The tenant's umask would hide the file from the panel's group.
	if err := f.Chmod(0o640); err != nil {
		return err
	}
	n, size, err := writeExport(f, os.Stdin, entries)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(partial, *out); err != nil {
		return err
	}
	ok = true
	fmt.Printf("exported %d files (%d bytes)\n", n, size)
	return nil
}

func parseExportMap(raw string) ([]exportEntry, error) {
	var entries []exportEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil || len(entries) == 0 {
		return nil, errors.New("FASTCP_EXPORT_MAP must be a non-empty JSON list")
	}
	for i := range entries {
		e := &entries[i]
		e.Src = strings.Trim(path.Clean("/"+e.Src), "/")
		dst := path.Clean(e.Dst)
		if e.Src == "" || dst == "." || path.IsAbs(dst) || dst == ".." || strings.HasPrefix(dst, "../") {
			return nil, fmt.Errorf("invalid export mapping %q -> %q", e.Src, e.Dst)
		}
		e.Dst = dst
	}
	// Longest source first so nested mappings win.
	sort.SliceStable(entries, func(i, j int) bool { return len(entries[i].Src) > len(entries[j].Src) })
	return entries, nil
}

// mapEntry returns the archive name for a tar entry, or "" to skip it.
func mapEntry(entries []exportEntry, name string) string {
	name = strings.Trim(path.Clean("/"+name), "/")
	for _, e := range entries {
		if name == e.Src {
			return e.Dst
		}
		if rest, ok := strings.CutPrefix(name, e.Src+"/"); ok {
			return path.Join(e.Dst, rest)
		}
	}
	return ""
}

func writeExport(w io.Writer, r io.Reader, entries []exportEntry) (files int, size int64, err error) {
	zw := zip.NewWriter(w)
	tr := tar.NewReader(r)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return files, size, fmt.Errorf("reading the snapshot: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		dst := mapEntry(entries, hdr.Name)
		if dst == "" || seen[dst] {
			continue
		}
		seen[dst] = true
		fh := &zip.FileHeader{Name: dst, Method: zip.Deflate, Modified: hdr.ModTime}
		fh.SetMode(os.FileMode(hdr.Mode).Perm())
		if strings.HasSuffix(dst, ".gz") || strings.HasSuffix(dst, ".zip") {
			fh.Method = zip.Store
		}
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return files, size, err
		}
		n, err := io.Copy(fw, tr)
		if err != nil {
			return files, size, err
		}
		files++
		size += n
	}
	if files == 0 {
		return 0, 0, errors.New("the snapshot contains none of the selected files")
	}
	return files, size, zw.Close()
}
