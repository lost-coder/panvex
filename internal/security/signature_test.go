package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedKeyParses ensures that the signing_key.pub we ship is well-formed
// ECDSA-P256. A malformed commit here would silently break every release path
// at runtime.
func TestEmbeddedKeyParses(t *testing.T) {
	pub, err := loadPublicKey()
	if err != nil {
		t.Fatalf("loadPublicKey() error = %v", err)
	}
	if pub.Curve != elliptic.P256() {
		t.Fatalf("embedded key curve = %v, want P-256", pub.Curve)
	}
}

// TestVerifyArtifactBytes_ValidSignature cross-checks our Verify implementation
// against a fresh signature produced in-test with the same embedded public key.
// Because we cannot embed the private key, the test signs with an ephemeral
// key and replaces the embedded PEM for the duration of the test.
func TestVerifyArtifactBytes_ValidSignature(t *testing.T) {
	artifact := []byte("hello panvex")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey error = %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	restore := signingKeyPEM
	signingKeyPEM = pubPEM
	t.Cleanup(func() { signingKeyPEM = restore })

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

func TestVerifyArtifactBytes_Mismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	restore := signingKeyPEM
	signingKeyPEM = pubPEM
	t.Cleanup(func() { signingKeyPEM = restore })

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
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	restore := signingKeyPEM
	signingKeyPEM = pubPEM
	t.Cleanup(func() { signingKeyPEM = restore })

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "artifact.bin")
	sigPath := filepath.Join(dir, "artifact.bin.sig")

	payload := []byte("panvex-agent-linux-amd64.tar.gz")
	if err := os.WriteFile(artifactPath, payload, 0644); err != nil {
		t.Fatalf("write artifact error = %v", err)
	}
	h := sha256.Sum256(payload)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
	if err := os.WriteFile(sigPath, sig, 0644); err != nil {
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
