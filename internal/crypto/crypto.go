// Package crypto encrypts the small secrets the panel keeps in its
// database, such as database passwords and backup credentials.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Purposes separate keys so a ciphertext stored for one use cannot be
// decrypted as another.
const (
	PurposeDatabasePassword = "database-password"
	PurposeBackup           = "backup"
)

const prefixV2 = "v2:"

// ErrDecrypt is returned for ciphertexts that are malformed or were not
// produced with this key and purpose.
var ErrDecrypt = errors.New("cannot decrypt stored secret")

// Box encrypts and decrypts secrets with keys derived from one master
// secret file.
type Box struct {
	master []byte
	legacy []byte // FastCP 2 key: SHA-256 of the master secret
}

// Load reads the master secret at path, creating it with mode 0600 if it
// does not exist.
func Load(path string) (*Box, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		data = []byte(base64.StdEncoding.EncodeToString(raw) + "\n")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create secret key: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return nil, fmt.Errorf("write secret key: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("read secret key: %w", err)
	}
	master, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode secret key: %w", err)
	}
	if len(master) < 16 {
		return nil, errors.New("secret key is too short")
	}
	return New(master), nil
}

// New returns a Box for the given master secret.
func New(master []byte) *Box {
	sum := sha256.Sum256(master)
	return &Box{master: append([]byte(nil), master...), legacy: sum[:]}
}

func (b *Box) key(purpose string) ([]byte, error) {
	return hkdf.Key(sha256.New, b.master, []byte("fastcp"), "fastcp/v2/"+purpose, 32)
}

func aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext for purpose.
func (b *Box) Seal(purpose, plaintext string) (string, error) {
	key, err := b.key(purpose)
	if err != nil {
		return "", err
	}
	gcm, err := aead(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(purpose))
	return prefixV2 + base64.RawURLEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal with the same purpose, or by
// FastCP 2.
func (b *Box) Open(purpose, ciphertext string) (string, error) {
	if rest, ok := strings.CutPrefix(ciphertext, prefixV2); ok {
		key, err := b.key(purpose)
		if err != nil {
			return "", err
		}
		data, err := base64.RawURLEncoding.DecodeString(rest)
		if err != nil {
			return "", ErrDecrypt
		}
		return open(key, data, []byte(purpose))
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrDecrypt
	}
	return open(b.legacy, data, nil)
}

func open(key, data, ad []byte) (string, error) {
	gcm, err := aead(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", ErrDecrypt
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, ad)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(pt), nil
}
