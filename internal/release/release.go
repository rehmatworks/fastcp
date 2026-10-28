// Package release downloads and verifies FastCP release artifacts.
//
// Every release publishes SHA256SUMS and SHA256SUMS.sig, a base64 Ed25519
// signature of SHA256SUMS made with the project's release key. Binaries are
// only accepted when the signature verifies against the public key built
// into this package and their SHA-256 matches the signed list.
package release

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	Repo            = "rehmatworks/fastcp"
	SumsFile        = "SHA256SUMS"
	SigFile         = "SHA256SUMS.sig"
	maxBinarySize   = 256 << 20
	maxMetadataSize = 1 << 20
)

//go:embed signing.pub
var publicKeyText string

// InstallerKeyRE matches the line of scripts/install.sh that holds the same
// public key, used by the installer to verify downloads with OpenSSL.
var InstallerKeyRE = regexp.MustCompile(`(?m)^FASTCP_SIGNING_PUBKEY="[^"\n]*"`)

// ErrNoKey means the binary was built without a release public key.
var ErrNoKey = errors.New("this build has no release signing key; update FastCP manually")

// PublicKey returns the embedded release verification key.
func PublicKey() (ed25519.PublicKey, error) {
	text := strings.TrimSpace(publicKeyText)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, ErrNoKey
		}
		return ed25519.PublicKey(raw), nil
	}
	return nil, ErrNoKey
}

// Sign returns the base64 signature line for data.
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

// Verify checks a base64 signature of data.
func Verify(pub ed25519.PublicKey, data, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("malformed release signature")
	}
	if !ed25519.Verify(pub, data, raw) {
		return errors.New("release signature verification failed")
	}
	return nil
}

// ParseSums reads sha256sum output into file name -> lowercase hex digest.
func ParseSums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("malformed checksum line %q", line)
		}
		sum, name := strings.ToLower(f[0]), strings.TrimPrefix(f[1], "*")
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != 64 {
			return nil, fmt.Errorf("malformed checksum for %s", name)
		}
		if name != filepath.Base(name) {
			return nil, fmt.Errorf("unexpected file name %q", name)
		}
		out[name] = sum
	}
	return out, sc.Err()
}

// AssetNames lists the binaries of a release for an architecture. The names
// are unchanged since FastCP 2 so older updaters can still install them.
func AssetNames(arch string) []string {
	return []string{"fastcp-linux-" + arch, "fastcp-agent-linux-" + arch}
}

// Client fetches releases from GitHub.
type Client struct {
	HTTP        *http.Client
	DownloadURL string // e.g. https://github.com/rehmatworks/fastcp/releases/download
	APIURL      string // e.g. https://api.github.com/repos/rehmatworks/fastcp
	// Key overrides the embedded public key (tests).
	Key ed25519.PublicKey
}

func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 10 * time.Minute},
		DownloadURL: "https://github.com/" + Repo + "/releases/download",
		APIURL:      "https://api.github.com/repos/" + Repo,
	}
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastCP-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (c *Client) getSmall(ctx context.Context, url string) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxMetadataSize {
		return nil, fmt.Errorf("GET %s: response too large", url)
	}
	return data, nil
}

// Latest returns the newest published release tag, e.g. "v3.0.1".
func (c *Client) Latest(ctx context.Context) (string, error) {
	data, err := c.getSmall(ctx, c.APIURL+"/releases/latest")
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", fmt.Errorf("unexpected release metadata: %w", err)
	}
	v, err := validate.ParseSemVer(rel.TagName)
	if err != nil {
		return "", err
	}
	return v.Tag(), nil
}

// Fetch downloads the named assets of release tag into dir after verifying
// the signed checksum list. Files are written with mode 0755.
func (c *Client) Fetch(ctx context.Context, tag, dir string, names []string) error {
	v, err := validate.ParseSemVer(tag)
	if err != nil {
		return err
	}
	pub := c.Key
	if pub == nil {
		if pub, err = PublicKey(); err != nil {
			return err
		}
	}
	base := c.DownloadURL + "/" + v.Tag() + "/"
	sums, err := c.getSmall(ctx, base+SumsFile)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	sig, err := c.getSmall(ctx, base+SigFile)
	if err != nil {
		return fmt.Errorf("downloading checksum signature: %w", err)
	}
	if err := Verify(pub, sums, sig); err != nil {
		return err
	}
	digests, err := ParseSums(sums)
	if err != nil {
		return err
	}
	for _, name := range names {
		want, ok := digests[name]
		if !ok {
			return fmt.Errorf("%s is not listed in the signed checksums", name)
		}
		if err := c.download(ctx, base+name, filepath.Join(dir, name), want); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) download(ctx context.Context, url, dest, wantHex string) error {
	resp, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinarySize+1))
	if err == nil && n > maxBinarySize {
		err = fmt.Errorf("%s is too large", filepath.Base(dest))
	}
	if err == nil {
		err = f.Chmod(0o755)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantHex {
		return fmt.Errorf("%s does not match its signed checksum", filepath.Base(dest))
	}
	return os.Rename(tmp, dest)
}
