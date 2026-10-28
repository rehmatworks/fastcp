package release

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data := []byte("abc  fastcp-linux-amd64\n")
	sig := Sign(priv, data)
	if err := Verify(pub, data, sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, append(data, 'x'), sig); err == nil {
		t.Fatal("tampered data verified")
	}
	if err := Verify(pub, data, []byte("nope")); err == nil {
		t.Fatal("garbage signature verified")
	}
}

func TestParseSums(t *testing.T) {
	sum := strings.Repeat("a", 64)
	got, err := ParseSums([]byte(sum + "  fastcp-linux-amd64\n" + sum + " *fastcp-agent-linux-amd64\n"))
	if err != nil || got["fastcp-linux-amd64"] != sum || got["fastcp-agent-linux-amd64"] != sum {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"xyz  a\n", sum + "  ../evil\n", sum + "\n"} {
		if _, err := ParseSums([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestEmbeddedPlaceholderHasNoKey(t *testing.T) {
	if strings.Contains(publicKeyText, "UNSET") {
		if _, err := PublicKey(); err != ErrNoKey {
			t.Fatalf("placeholder key should yield ErrNoKey, got %v", err)
		}
	}
}

func TestInstallerKeyMatchesEmbedded(t *testing.T) {
	script, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := InstallerKeyRE.Find(script)
	if m == nil {
		t.Fatal("scripts/install.sh has no FASTCP_SIGNING_PUBKEY line")
	}
	installerKey := strings.TrimSuffix(strings.TrimPrefix(string(m), `FASTCP_SIGNING_PUBKEY="`), `"`)
	embedded := "UNSET"
	if pub, err := PublicKey(); err == nil {
		embedded = base64.StdEncoding.EncodeToString(pub)
	}
	if installerKey != embedded {
		t.Fatalf("scripts/install.sh key %q does not match internal/release/signing.pub %q", installerKey, embedded)
	}
}

func newServer(t *testing.T, priv ed25519.PrivateKey, files map[string][]byte, tamper string) *httptest.Server {
	var sums strings.Builder
	for name, data := range files {
		h := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	sumBytes := []byte(sums.String())
	sig := Sign(priv, sumBytes)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch {
		case r.URL.Path == "/api/releases/latest":
			w.Write([]byte(`{"tag_name":"v3.1.0"}`))
		case name == SumsFile:
			w.Write(sumBytes)
		case name == SigFile:
			w.Write(sig)
		case name == tamper:
			w.Write(append(files[name], 'x'))
		case files[name] != nil:
			w.Write(files[name])
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFetch(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	files := map[string][]byte{"fastcp-linux-amd64": []byte("panel"), "fastcp-agent-linux-amd64": []byte("agent")}

	srv := newServer(t, priv, files, "")
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), DownloadURL: srv.URL + "/dl", APIURL: srv.URL + "/api", Key: pub}
	tag, err := c.Latest(context.Background())
	if err != nil || tag != "v3.1.0" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
	dir := t.TempDir()
	if err := c.Fetch(context.Background(), tag, dir, AssetNames("amd64")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "fastcp-agent-linux-amd64"))
	if string(data) != "agent" {
		t.Fatalf("got %q", data)
	}

	bad := newServer(t, priv, files, "fastcp-linux-amd64")
	defer bad.Close()
	c.DownloadURL, c.HTTP = bad.URL+"/dl", bad.Client()
	if err := c.Fetch(context.Background(), tag, t.TempDir(), AssetNames("amd64")); err == nil {
		t.Fatal("tampered binary accepted")
	}

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	c.DownloadURL, c.HTTP, c.Key = srv.URL+"/dl", srv.Client(), otherPub
	if err := c.Fetch(context.Background(), tag, t.TempDir(), AssetNames("amd64")); err == nil {
		t.Fatal("signature from another key accepted")
	}
	if err := c.Fetch(context.Background(), "../../etc", t.TempDir(), AssetNames("amd64")); err == nil {
		t.Fatal("invalid tag accepted")
	}
}
