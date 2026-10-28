package validate

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHPublicKey is a parsed and normalized authorized_keys entry.
type SSHPublicKey struct {
	// Line is the key in canonical "type base64" form, without options or comment.
	Line        string
	Type        string
	Fingerprint string
}

// ParseSSHPublicKey accepts a single OpenSSH public key line. Options are
// rejected because they would let a tenant add restrictions or commands that
// FastCP cannot display. DSA keys and RSA keys shorter than 2048 bits are
// rejected because current OpenSSH no longer accepts them.
func ParseSSHPublicKey(raw string) (*SSHPublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("public key is required")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return nil, errors.New("paste exactly one public key")
	}
	if len(raw) > 16*1024 {
		return nil, errors.New("public key is too long")
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(raw))
	if err != nil {
		return nil, errors.New("not a valid OpenSSH public key; paste the contents of your .pub file")
	}
	if len(options) > 0 {
		return nil, errors.New("authorized_keys options are not supported")
	}
	if len(strings.TrimSpace(string(rest))) > 0 {
		return nil, errors.New("paste exactly one public key")
	}
	switch pub.Type() {
	case ssh.KeyAlgoDSA:
		return nil, errors.New("DSA keys are not supported; use an Ed25519 or RSA key")
	case ssh.KeyAlgoRSA:
		if ck, ok := pub.(ssh.CryptoPublicKey); ok {
			if rk, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok && rk.N.BitLen() < 2048 {
				return nil, fmt.Errorf("RSA keys must be at least 2048 bits (this one is %d)", rk.N.BitLen())
			}
		}
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	return &SSHPublicKey{
		Line:        line,
		Type:        pub.Type(),
		Fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}
