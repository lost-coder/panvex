package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedKeyParses ensures every signing_key*.pub we ship is well-formed
// ECDSA-P256. A malformed commit here would silently break every release
// path at runtime.
func TestEmbeddedKeyParses(t *testing.T) {
	keys, err := loadPublicKeys()
	if err != nil {
		t.Fatalf("loadPublicKeys() error = %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("loadPublicKeys() returned no keys")
	}
	for i, pub := range keys {
		if pub.Curve != elliptic.P256() {
			t.Fatalf("embedded key #%d curve = %v, want P-256", i, pub.Curve)
		}
	}
}

// TestVerifyArtifactBytes_ValidSignature cross-checks the Verify implementation
// against a fresh signature produced in-test. The matching public key is
// installed via setKeysForTesting because we cannot embed a private key.
func TestVerifyArtifactBytes_ValidSignature(t *testing.T) {
	artifact := []byte("hello panvex")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	t.Cleanup(setKeysForTesting(&priv.PublicKey))

	h := sha256.Sum256(artifact)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, h[:])
	if err != nil {
		t.Fatalf("SignASN1 error = %v", err)
	}

	if err := VerifyArtifactBytes(artifact, sig); err != nil {
		t.Fatalf("VerifyArtifactBytes raw DER error = %v, want nil", err)
	}

	// Same signature, base64-encoded (cosign output shape) must also verify.
	b64 := []byte(base64.StdEncoding.EncodeToString(sig))
	if err := VerifyArtifactBytes(artifact, b64); err != nil {
		t.Fatalf("VerifyArtifactBytes base64 error = %v, want nil", err)
	}
}

// TestVerifyArtifactBytes_MultiKeyTrust covers the rotation case: the
// trust set carries TWO valid keys. A signature from either must verify.
func TestVerifyArtifactBytes_MultiKeyTrust(t *testing.T) {
	priv1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	priv2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t.Cleanup(setKeysForTesting(&priv1.PublicKey, &priv2.PublicKey))

	artifact := []byte("multi-key payload")
	h := sha256.Sum256(artifact)

	for i, priv := range []*ecdsa.PrivateKey{priv1, priv2} {
		sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
		if err := VerifyArtifactBytes(artifact, sig); err != nil {
			t.Fatalf("key #%d signature must verify, got %v", i, err)
		}
	}
}

// TestVerifyArtifactBytes_RejectsUntrustedKey ensures the multi-key loop
// does NOT accept a signature from a key outside the trust set.
func TestVerifyArtifactBytes_RejectsUntrustedKey(t *testing.T) {
	trusted, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	untrusted, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t.Cleanup(setKeysForTesting(&trusted.PublicKey))

	artifact := []byte("payload")
	h := sha256.Sum256(artifact)
	sig, _ := ecdsa.SignASN1(rand.Reader, untrusted, h[:])

	if err := VerifyArtifactBytes(artifact, sig); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("err = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifyArtifactBytes_Mismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t.Cleanup(setKeysForTesting(&priv.PublicKey))

	h := sha256.Sum256([]byte("original"))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])

	if err := VerifyArtifactBytes([]byte("tampered"), sig); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("VerifyArtifactBytes error = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifyArtifactBytes_MissingSignature(t *testing.T) {
	if err := VerifyArtifactBytes([]byte("x"), nil); !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("err = %v, want ErrSignatureMissing", err)
	}
}

func TestVerifyArtifactFile(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t.Cleanup(setKeysForTesting(&priv.PublicKey))

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "artifact.bin")
	sigPath := filepath.Join(dir, "artifact.bin.sig")

	payload := []byte("panvex-agent-linux-amd64.tar.gz")
	if err := os.WriteFile(artifactPath, payload, 0600); err != nil {
		t.Fatalf("write artifact error = %v", err)
	}
	h := sha256.Sum256(payload)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
	if err := os.WriteFile(sigPath, sig, 0600); err != nil {
		t.Fatalf("write signature error = %v", err)
	}

	if err := VerifyArtifactFile(artifactPath, sigPath); err != nil {
		t.Fatalf("VerifyArtifactFile error = %v, want nil", err)
	}
}

func TestVerifyArtifactFile_MissingSigPath(t *testing.T) {
	if err := VerifyArtifactFile("/nowhere", ""); !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("err = %v, want ErrSignatureMissing", err)
	}
}
