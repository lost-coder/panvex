// Package security provides primitives for verifying release-artifact
// signatures. The public key is embedded at build time; release artifacts
// (control-plane and agent binaries) must carry a detached ECDSA-P256 / SHA-256
// signature produced by the matching private key for the update subsystem to
// install them.
//
// The key pair is compatible with both `openssl dgst -sha256 -sign` (raw DER
// output) and `cosign sign-blob --key ... --output-signature <file>` (base64
// DER output). VerifyArtifactSignature accepts either form.
package security

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

//go:embed signing_key.pub
var signingKeyPEM []byte

// ErrSignatureMissing indicates that no detached signature was supplied when
// one was required. Callers must treat a missing signature as fatal — the
// update subsystem refuses to proceed without it.
var ErrSignatureMissing = errors.New("release signature is missing")

// ErrSignatureMismatch indicates that the artifact bytes failed verification
// against the embedded public key. Callers must not install the artifact.
var ErrSignatureMismatch = errors.New("release signature verification failed")

// loadPublicKey parses the embedded PEM-encoded ECDSA P-256 public key. The
// PEM is loaded once at init; if the embedded value is malformed we return an
// error at the call site rather than panicking so tests can observe the
// failure.
func loadPublicKey() (*ecdsa.PublicKey, error) {
	if len(signingKeyPEM) == 0 {
		return nil, errors.New("embedded signing key is empty")
	}
	block, _ := pem.Decode(signingKeyPEM)
	if block == nil {
		return nil, errors.New("embedded signing key is not a valid PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded signing key is not ECDSA (got %T)", pub)
	}
	return ecdsaPub, nil
}

// VerifyArtifactBytes validates that sig (either raw DER or base64-encoded
// DER) is a valid ECDSA signature over sha256(artifact) under the embedded
// public key. Returns nil on success, ErrSignatureMismatch on failure.
func VerifyArtifactBytes(artifact, sig []byte) error {
	if len(sig) == 0 {
		return ErrSignatureMissing
	}
	pub, err := loadPublicKey()
	if err != nil {
		return err
	}

	sigBytes := decodeSignature(sig)
	hash := sha256.Sum256(artifact)
	if !ecdsa.VerifyASN1(pub, hash[:], sigBytes) {
		return ErrSignatureMismatch
	}
	return nil
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

	pub, err := loadPublicKey()
	if err != nil {
		return err
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
	if !ecdsa.VerifyASN1(pub, digest, sigBytes) {
		return ErrSignatureMismatch
	}
	return nil
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
