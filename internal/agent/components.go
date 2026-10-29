package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/rehmatworks/fastcp/internal/validate"
)

// Third-party components FastCP installs. The agent upgrades older copies
// when it starts.
const (
	// CaddyVersion is the upstream release FastCP's Caddy is built from
	// (see caddy/ at the repository root). CaddyBuild is what that build
	// reports; it is published with each FastCP release, signed like the
	// FastCP binaries, and the agent installs it whenever another version
	// is installed.
	CaddyVersion = "2.11.7"
	CaddyBuild   = CaddyVersion + "-fastcp.1"

	// phpMyAdmin is pinned by version and SHA-256 of the release archive.
	PMAVersion = "5.2.3"
	pmaSHA256  = "12ba1c425fa4071abbd4e7668c9ebdeac0b0755a467a6d6d5026122bb47c102b"

	componentMaxBytes = 128 << 20
)

// caddyAsset is the name of FastCP's Caddy build in a FastCP release.
func caddyAsset(arch string) string { return "caddy-linux-" + arch }

func pmaURL() string {
	return fmt.Sprintf("https://files.phpmyadmin.net/phpMyAdmin/%[1]s/phpMyAdmin-%[1]s-all-languages.tar.gz", PMAVersion)
}

// parseCaddyVersion returns the version from `caddy version` output, without
// the leading "v". Caddy logs a warning before it when it has no home
// directory, which is the case under systemd.
func parseCaddyVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if _, err := validate.ParseSemVer(f[0]); err == nil {
			return strings.TrimPrefix(f[0], "v")
		}
	}
	return ""
}

// needsUpgrade reports whether installed should be replaced by want. A
// missing component is installed; a version that cannot be parsed (a custom
// build, for example) is left alone.
func needsUpgrade(installed, want string) bool {
	if installed == "" {
		return true
	}
	have, err := validate.ParseSemVer(installed)
	if err != nil {
		return false
	}
	pinned, err := validate.ParseSemVer(want)
	if err != nil {
		return false
	}
	return have.Compare(pinned) < 0
}

// downloadVerified saves url to a new file in dir and checks its SHA-256.
// The caller removes the returned file.
func downloadVerified(ctx context.Context, url, wantHex, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FastCP")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, componentMaxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > componentMaxBytes {
		err = fmt.Errorf("GET %s: response too large", url)
	}
	if err == nil {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantHex) {
			err = fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantHex)
		}
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// extractTarGz unpacks the regular files and directories of a .tar.gz into
// dest, dropping the first strip path components. Links, devices and paths
// that leave dest are rejected. Files keep only their executable bit.
func extractTarGz(archive, dest string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name, ok := stripComponents(hdr.Name, strip)
		if !ok {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if dir := path.Dir(name); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			perm := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				perm = 0o755
			}
			out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, componentMaxBytes))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
		default:
			return fmt.Errorf("unexpected entry %q in archive", hdr.Name)
		}
	}
}

// stripComponents removes the first n elements of a slash-separated archive
// path. It reports false for entries that are consumed entirely or unsafe.
func stripComponents(name string, n int) (string, bool) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	parts := strings.Split(clean, "/")
	if len(parts) <= n {
		return "", false
	}
	return strings.Join(parts[n:], "/"), true
}
