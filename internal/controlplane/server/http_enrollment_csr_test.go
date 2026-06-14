package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/security"
)

// buildTestCSRPEM generates an ephemeral ECDSA P-256 keypair and returns both
// the private key and a DER-signed CSR PEM with the given CN.
func buildTestCSRPEM(t *testing.T, cn string) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("buildTestCSRPEM: generate key: %v", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"Panvex Agents"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatalf("buildTestCSRPEM: create CSR: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	return key, csrPEM
}

// TestAgentBootstrapRequiresCSRAndReturnsNoPrivateKey pins A9:
//  1. POST /api/agent/bootstrap WITHOUT csr_pem → 400.
//  2. POST WITH a valid csr_pem → 200; response has certificate_pem that pairs
//     with the locally-generated key, and the response body contains NO
//     private_key_pem field.
func TestAgentBootstrapRequiresCSRAndReturnsNoPrivateKey(t *testing.T) {
	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	// 1. Missing csr_pem → 400.
	missingCSR := performJSONRequestWithHeaders(
		t,
		server,
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-csr-test",
			"version":   "1.0.0",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
		},
	)
	if missingCSR.Code != http.StatusBadRequest {
		t.Fatalf("POST without csr_pem: status = %d, want %d", missingCSR.Code, http.StatusBadRequest)
	}

	// Refresh token (the missing-CSR request must not consume it).
	token2, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() (2nd) error = %v", err)
	}

	// 2. Valid csr_pem → 200, cert pairs with local key, no private_key_pem.
	localKey, csrPEM := buildTestCSRPEM(t, "arbitrary-cn")
	withCSR := performJSONRequestWithHeaders(
		t,
		server,
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-csr-test",
			"version":   "1.0.0",
			"csr_pem":   csrPEM,
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token2.Value,
		},
	)
	if withCSR.Code != http.StatusOK {
		t.Fatalf("POST with csr_pem: status = %d, want %d; body = %s",
			withCSR.Code, http.StatusOK, withCSR.Body.String())
	}

	// Decode response into a map to assert absence of private_key_pem.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(withCSR.Body.Bytes(), &raw); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if _, present := raw["private_key_pem"]; present {
		t.Fatal("response contains private_key_pem, want it absent (A9: key never crosses the wire)")
	}

	certPEMRaw, ok := raw["certificate_pem"]
	if !ok {
		t.Fatal("response missing certificate_pem")
	}
	var certPEM string
	if err := json.Unmarshal(certPEMRaw, &certPEM); err != nil {
		t.Fatalf("json.Unmarshal(certificate_pem) error = %v", err)
	}
	if certPEM == "" {
		t.Fatal("certificate_pem is empty")
	}

	// Marshal local key to PEM so we can call tls.X509KeyPair.
	keyDER, err := x509.MarshalECPrivateKey(localKey)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))

	if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
		t.Fatalf("issued cert does not pair with locally-generated key: %v", err)
	}
}
