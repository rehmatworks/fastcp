// Command releasekey manages the FastCP release signing key.
//
//	go run ./tools/releasekey gen -pub internal/release/signing.pub -installer scripts/install.sh
//	    Generates a key pair and writes the public key to both files. The
//	    private key is printed once: store it as the FASTCP_SIGNING_KEY
//	    repository secret and never commit it.
//	FASTCP_SIGNING_KEY=... go run ./tools/releasekey sign SHA256SUMS
//	    Writes SHA256SUMS.sig.
//	go run ./tools/releasekey verify SHA256SUMS [SHA256SUMS.sig]
//	    Verifies a signature against the embedded public key.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rehmatworks/fastcp/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = gen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasekey:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: releasekey gen [-pub FILE] [-installer FILE] | sign FILE | verify FILE [SIG]")
	os.Exit(2)
}

func gen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	pubPath := fs.String("pub", "", "write the public key to this file (e.g. internal/release/signing.pub)")
	installer := fs.String("installer", "", "set FASTCP_SIGNING_PUBKEY in this installer script (e.g. scripts/install.sh)")
	fs.Parse(args)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pubText := base64.StdEncoding.EncodeToString(pub)
	if *installer != "" {
		script, err := os.ReadFile(*installer)
		if err != nil {
			return err
		}
		if !release.InstallerKeyRE.Match(script) {
			return fmt.Errorf("%s has no FASTCP_SIGNING_PUBKEY line", *installer)
		}
		script = release.InstallerKeyRE.ReplaceAll(script, []byte(`FASTCP_SIGNING_PUBKEY="`+pubText+`"`))
		if err := os.WriteFile(*installer, script, 0o755); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote public key to %s\n", *installer)
	}
	if *pubPath != "" {
		content := "# FastCP release signing public key (Ed25519, base64).\n" + pubText + "\n"
		if err := os.WriteFile(*pubPath, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote public key to %s\n", *pubPath)
	}
	fmt.Println("Public key (commit this to internal/release/signing.pub and scripts/install.sh):")
	fmt.Println(pubText)
	fmt.Println()
	fmt.Println("Private key (add as the FASTCP_SIGNING_KEY GitHub secret; do NOT commit):")
	fmt.Println(base64.StdEncoding.EncodeToString(priv.Seed()))
	return nil
}

func privateKey() (ed25519.PrivateKey, error) {
	text := strings.TrimSpace(os.Getenv("FASTCP_SIGNING_KEY"))
	if text == "" {
		return nil, errors.New("FASTCP_SIGNING_KEY is not set")
	}
	seed, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("FASTCP_SIGNING_KEY is not a base64 Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func sign(args []string) error {
	if len(args) != 1 {
		usage()
	}
	priv, err := privateKey()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	if pub, err := release.PublicKey(); err == nil && !pub.Equal(priv.Public()) {
		return errors.New("FASTCP_SIGNING_KEY does not match internal/release/signing.pub")
	}
	return os.WriteFile(args[0]+".sig", release.Sign(priv, data), 0o644)
}

func verify(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		usage()
	}
	sigPath := args[0] + ".sig"
	if len(args) == 2 {
		sigPath = args[1]
	}
	pub, err := release.PublicKey()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	if err := release.Verify(pub, data, sig); err != nil {
		return err
	}
	fmt.Println("signature OK")
	return nil
}
