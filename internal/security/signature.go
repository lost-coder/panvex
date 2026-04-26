// Package security provides primitives for verifying release-artifact
// signatures. Public keys are embedded at build time; release artifacts
// (control-plane and agent binaries) must carry a detached ECDSA-P256 / SHA-256
// signature produced by the matching private key for the update subsystem to
// install them.
//
// The key pair is compatible with both `openssl dgst -sha256 -sign` (raw DER
// output) and `cosign sign-blob --key ... --output-signature <file>` (base64
// DER output). VerifyArtifactSignature accepts either form.
//
// Multi-key trust: every `signing_key*.pub` PEM file in this directory is
// embedded and treated as a trusted signer. Rotation procedure:
//   1. Drop the new public key alongside the existing one (e.g. signing_key2.pub).
//   2. Sign the next release with EITHER the old or the new private key.
//   3. Once every agent has updated past that release (the new key now ships
//      embedded everywhere), delete the old key in a follow-up release.
// This avoids a single-step rotation that would brick agents still on the
// prior trust anchor.
package security

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
)

//go:embed signing_key*.pub
var signingKeysFS embed.FS

// ErrSignatureMissing indicates that no detached signature was supplied when
// one was required. Callers must treat a missing signature as fatal — the
// update subsystem refuses to proceed without it.
var ErrSignatureMissing = errors.New("release signature is missing")

// ErrSignatureMismatch indicates that the artifact bytes failed verification
// against every embedded public key. Callers must not install the artifact.
var ErrSignatureMismatch = errors.New("release signature verification failed")

var (
	publicKeysOnce sync.Once
	publicKeys     []*ecdsa.PublicKey
	publicKeysErr  error
)

// loadPublicKeys parses every embedded `signing_key*.pub` PEM block. The
// result is cached on first call. An empty key set or any malformed PEM
// surfaces as an error so the update subsystem fails closed (refuses to
// verify) instead of silently accepting unsigned artifacts.
func loadPublicKeys() ([]*ecdsa.PublicKey, error) {
	publicKeysOnce.Do(func() {
		entries, err := fs.ReadDir(signingKeysFS, ".")
		if err != nil {
			publicKeysErr = fmt.Errorf("read embedded signing keys: %w", err)
			return
		}
		// Deterministic order makes failure messages reproducible across
		// builds and guarantees the dominant key (alphabetically first
		// filename) is tried first when multiple are present.
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasPrefix(e.Name(), "signing_key") || !strings.HasSuffix(e.Name(), ".pub") {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		if len(names) == 0 {
			publicKeysErr = errors.New("no embedded signing keys (signing_key*.pub) found")
			return
		}

		keys := make([]*ecdsa.PublicKey, 0, len(names))
		for _, name := range names {
			raw, err := fs.ReadFile(signingKeysFS, name)
			if err != nil {
				publicKeysErr = fmt.Errorf("read %s: %w", name, err)
				return
			}
			pub, err := parsePublicKey(name, raw)
			if err != nil {
				publicKeysErr = err
				return
			}
			keys = append(keys, pub)
		}
		publicKeys = keys
	})
	return publicKeys, publicKeysErr
}

func parsePublicKey(name string, raw []byte) (*ecdsa.PublicKey, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%s is not a valid PEM block", name)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s is not ECDSA (got %T)", name, pub)
	}
	return ecdsaPub, nil
}

// verifyAgainstKeys returns nil if any embedded key validates the digest +
// signature pair, ErrSignatureMismatch otherwise.
func verifyAgainstKeys(digest, sigBytes []byte) error {
	keys, err := loadPublicKeys()
	if err != nil {
		return err
	}
	for _, pub := range keys {
		if ecdsa.VerifyASN1(pub, digest, sigBytes) {
			return nil
		}
	}
	return ErrSignatureMismatch
}

// VerifyArtifactBytes validates that sig (either raw DER or base64-encoded
// DER) is a valid ECDSA signature over sha256(artifact) under any of the
// embedded public keys. Returns nil on success, ErrSignatureMismatch on
// failure to match any key.
func VerifyArtifactBytes(artifact, sig []byte) error {
	if len(sig) == 0 {
		return ErrSignatureMissing
	}
	sigBytes := decodeSignature(sig)
	hash := sha256.Sum256(artifact)
	return verifyAgainstKeys(hash[:], sigBytes)
}

// VerifyArtifactFile is the file-based counterpart to VerifyArtifactBytes: it
// streams artifactPath to compute the hash (so large archives do not need to
// be fully loaded into memory) and reads sigPath for the detached signature.
func VerifyArtifactFile(artifactPath, sigPath string) error {
	if sigPath == "" {
		return ErrSignatureMissing
	}
	sig, err := os.ReadFile(sigPath) //nolint:gosec // operator-controlled path resolved by caller
	if err != nil {
		return fmt.Errorf("read signature file: %w", err)
	}
	if len(sig) == 0 {
		return ErrSignatureMissing
	}

	f, err := os.Open(artifactPath) //nolint:gosec // operator-controlled path
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("hash artifact: %w", err)
	}
	digest := hasher.Sum(nil)

	sigBytes := decodeSignature(sig)
	return verifyAgainstKeys(digest, sigBytes)
}

// setKeysForTesting swaps the in-package trusted-keys set for the duration
// of a test. Tests cannot embed a private key (we don't ship one), so they
// generate an ephemeral ECDSA-P256 keypair and call this setter to make
// the public side trusted while the matching private side signs the
// artifact. The returned cleanup must be invoked (typically via t.Cleanup)
// to restore the production trust set.
//
// Unexported on purpose — production callers must never reach this.
func setKeysForTesting(keys ...*ecdsa.PublicKey) func() {
	publicKeysOnce.Do(func() {}) // pin once.Do so we don't re-load production keys mid-test
	prev := publicKeys
	prevErr := publicKeysErr
	publicKeys = append([]*ecdsa.PublicKey(nil), keys...)
	publicKeysErr = nil
	return func() {
		publicKeys = prev
		publicKeysErr = prevErr
	}
}

// decodeSignature accepts either a raw DER-encoded ECDSA signature (openssl
// dgst -sign output) or a base64-encoded DER signature (cosign sign-blob
// --output-signature output). Leading/trailing whitespace in the base64 form
// is tolerated so newline-terminated files from common tooling work.
func decodeSignature(sig []byte) []byte {
	trimmed := strings.TrimSpace(string(sig))
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) > 0 {
		// ASN.1 DER sequences begin with 0x30; if the base64 output decodes to
		// something starting with 0x30 we treat it as a valid DER signature.
		if decoded[0] == 0x30 {
			return decoded
		}
	}
	return sig
}
