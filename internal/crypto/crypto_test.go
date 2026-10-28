package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	b := New([]byte("0123456789abcdef0123456789abcdef"))
	ct, err := b.Seal(PurposeDatabasePassword, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Open(PurposeDatabasePassword, ct)
	if err != nil || got != "s3cret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := b.Open(PurposeBackup, ct); err == nil {
		t.Fatal("a ciphertext opened under another purpose")
	}
	other := New([]byte("fedcba9876543210fedcba9876543210"))
	if _, err := other.Open(PurposeDatabasePassword, ct); err == nil {
		t.Fatal("a ciphertext opened with another key")
	}
}

func TestOpenLegacy(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	sum := sha256.Sum256(master)
	block, _ := aes.NewCipher(sum[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	legacy := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte("old"), nil))

	got, err := New(master).Open(PurposeDatabasePassword, legacy)
	if err != nil || got != "old" {
		t.Fatalf("Open(legacy) = %q, %v", got, err)
	}
}

func TestLoadCreatesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".secret")
	b1, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode = %v, %v", st.Mode(), err)
	}
	ct, _ := b1.Seal(PurposeBackup, "x")
	b2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Open(PurposeBackup, ct); err != nil || got != "x" {
		t.Fatalf("reloaded key cannot open: %q, %v", got, err)
	}
}
