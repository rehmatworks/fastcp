package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const secretKeyPath = "/opt/fastcp/data/.secret"

var (
	encryptionKey []byte
	keyOnce       sync.Once
	keyErr        error
)

// getKey loads the encryption key from the secret file
func getKey() ([]byte, error) {
	keyOnce.Do(func() {
		data, err := os.ReadFile(secretKeyPath)
		if err != nil {
			keyErr = fmt.Errorf("failed to read secret key: %w", err)
			return
		}
		// Decode base64 and hash to get 32 bytes for AES-256
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			keyErr = fmt.Errorf("failed to decode secret key: %w", err)
			return
		}
		hash := sha256.Sum256(decoded)
		encryptionKey = hash[:]
	})
	return encryptionKey, keyErr
}

// Encrypt encrypts plaintext using AES-GCM and returns standard base64
func Encrypt(plaintext string) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// EncryptURLSafe encrypts plaintext using AES-GCM and returns URL-safe base64
func EncryptURLSafe(plaintext string) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-GCM (standard base64)
func Decrypt(ciphertext string) (string, error) {
	return decryptWithEncoding(ciphertext, base64.StdEncoding)
}

// DecryptURLSafe decrypts ciphertext using AES-GCM (URL-safe base64)
func DecryptURLSafe(ciphertext string) (string, error) {
	return decryptWithEncoding(ciphertext, base64.URLEncoding)
}

func decryptWithEncoding(ciphertext string, encoding *base64.Encoding) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}

	data, err := encoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
