package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// seedLegacyEnc1CA plants a CertificateAuthorityRecord whose private key is
// stored with a raw "ENC:" prefix (simulating a pre-release ENC:v1 blob)
// without any real encryption — the value is just prefixed so the loader
// recognises and rejects it. The CA certificate itself is a freshly-generated
// P-256 ECDSA root with a long-enough validity that loadOrCreateCertificateAuthority
// does not regenerate it during the test.
func seedLegacyEnc1CA(t *testing.T, store storage.CertificateAuthorityStore, now time.Time) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Panvex Test"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, priv.Public(), priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	// Plant an ENC:-prefixed blob (pre-release format). The content is
	// intentionally opaque — we only need the prefix to trigger rejection.
	caPEM := encodePEM("CERTIFICATE", der)
	if err := store.PutCertificateAuthority(context.Background(), storage.CertificateAuthorityRecord{
		CAPEM:         caPEM,
		PrivateKeyPEM: encryptedPEMPrefix + "AAAA",
		UpdatedAt:     now.UTC(),
	}); err != nil {
		t.Fatalf("PutCertificateAuthority: %v", err)
	}
}

// TestLegacyEnc1BlobFailsLoud verifies that a stored CA record with the
// pre-release "ENC:" prefix causes loadOrCreateCertificateAuthority to fail
// with a loud error mentioning "no longer supported", both with and without a
// passphrase. The record must NEVER be silently treated as plaintext.
func TestLegacyEnc1BlobFailsLoud(t *testing.T) {
	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	seedLegacyEnc1CA(t, store, now)

	// With a passphrase: decryptPEM must reject ENC: loudly.
	_, err = loadOrCreateCertificateAuthority(context.Background(), store, now, "some-passphrase")
	if err == nil {
		t.Fatal("loadOrCreateCertificateAuthority(ENC:v1, with key) error = nil, want loud rejection")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("error = %q, want mention of removed ENC:v1 support", err)
	}

	// Without a passphrase: the blob must also be rejected (not mistaken for plaintext).
	_, err = loadOrCreateCertificateAuthority(context.Background(), store, now, "")
	if err == nil {
		t.Fatal("loadOrCreateCertificateAuthority(ENC:v1, no key) error = nil, want loud rejection")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("error (no key) = %q, want mention of removed ENC:v1 support", err)
	}
}

// authorityCancellationStore is a CertificateAuthorityStore stub that
// returns ctx.Err() from Get/Put when the supplied ctx is already
// cancelled. Used to pin Plan 3 Task 3: the CA loader must propagate
// caller ctx instead of falling back to context.Background().
type authorityCancellationStore struct{}

func (authorityCancellationStore) GetCertificateAuthority(ctx context.Context) (storage.CertificateAuthorityRecord, error) {
	if err := ctx.Err(); err != nil {
		return storage.CertificateAuthorityRecord{}, err
	}
	return storage.CertificateAuthorityRecord{}, storage.ErrNotFound
}

func (authorityCancellationStore) PutCertificateAuthority(ctx context.Context, _ storage.CertificateAuthorityRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// TestLoadOrCreateCertificateAuthority_RespectsContextCancellation pins
// Plan 3 Task 3: the CA loader must propagate caller ctx so a Close()
// during a wedged GetCertificateAuthority aborts the storage call.
func TestLoadOrCreateCertificateAuthority_RespectsContextCancellation(t *testing.T) {
	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadOrCreateCertificateAuthority(ctx, authorityCancellationStore{}, now, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadOrCreateCertificateAuthority error = %v, want context.Canceled", err)
	}
}

func TestAuthorityIssuesPanelClientCertificate(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	if len(authority.clientCertificate.Certificate) < 2 {
		t.Fatalf("panel client cert chain length = %d, want >= 2 (leaf + CA for bootstrap pin verifier)", len(authority.clientCertificate.Certificate))
	}
	leaf, err := x509.ParseCertificate(authority.clientCertificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse client leaf: %v", err)
	}
	if leaf.Subject.CommonName != PanelClientCN {
		t.Errorf("client cert CN = %q, want %q", leaf.Subject.CommonName, PanelClientCN)
	}
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Error("panel client cert must carry ClientAuth EKU")
	}
	if slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Error("panel client cert must NOT carry ServerAuth EKU (outbound-dial-only identity)")
	}

	cfg := authority.outboundTLSConfig()
	if cfg.RootCAs == nil {
		t.Error("outbound TLS config must trust the panel CA via RootCAs")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("outbound TLS config Certificates = %d, want 1 (panel client cert)", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Error("outbound TLS config must require TLS 1.3")
	}
	if cfg.ServerName != "" {
		t.Error("base outbound config must leave ServerName empty (set per-dial by the supervisor)")
	}
}

// newCAStoreForTest opens a fresh in-memory SQLite store scoped to the test.
func newCAStoreForTest(t *testing.T) storage.CertificateAuthorityStore {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedExpiredCertificateAuthority plants a CA record whose certificate has
// already expired (NotAfter in the past) so the expiry check triggers.
func seedExpiredCertificateAuthority(t *testing.T, store storage.CertificateAuthorityStore) {
	t.Helper()
	// Build a real CA that was valid 10 years ago and expired 5 years ago.
	past := time.Now().Add(-10 * 365 * 24 * time.Hour)
	authority, err := newCertificateAuthority(past)
	if err != nil {
		t.Fatalf("newCertificateAuthority(past): %v", err)
	}
	// Test fixture seeds a plaintext CA record directly — opt into the 3.1
	// escape hatch rather than exercising real encryption here.
	t.Setenv(EnvAllowPlaintextCA, "1")
	rec, err := authority.record(context.Background(), past, "")
	if err != nil {
		t.Fatalf("authority.record: %v", err)
	}
	if err := store.PutCertificateAuthority(context.Background(), rec); err != nil {
		t.Fatalf("PutCertificateAuthority: %v", err)
	}
}

// TestExpiredCAFailsLoudInsteadOfSilentRegen: an expired CA must abort
// startup with an actionable error. Silent regeneration invalidated the
// whole fleet without warning (audit 2026-06-09, A5 follow-up).
func TestExpiredCAFailsLoudInsteadOfSilentRegen(t *testing.T) {
	store := newCAStoreForTest(t)
	seedExpiredCertificateAuthority(t, store)
	_, err := loadOrCreateCertificateAuthority(context.Background(), store, time.Now(), "")
	if err == nil {
		t.Fatal("expired CA: err = nil, want loud startup failure")
	}
	if !strings.Contains(err.Error(), "rotate-ca") {
		t.Fatalf("err = %q, want recovery instruction mentioning rotate-ca", err)
	}
	// The stored record must be untouched (no silent overwrite).
	rec, getErr := store.GetCertificateAuthority(context.Background())
	if getErr != nil || rec.PrivateKeyPEM == "" {
		t.Fatalf("stored CA must survive: %v", getErr)
	}
}

func TestRotateCertificateAuthorityReplacesRecord(t *testing.T) {
	store := newCAStoreForTest(t)
	seedExpiredCertificateAuthority(t, store)
	before, _ := store.GetCertificateAuthority(context.Background())
	if err := RotateCertificateAuthority(context.Background(), store, time.Now(), ""); err != nil {
		t.Fatalf("RotateCertificateAuthority: %v", err)
	}
	after, err := store.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("get after rotate: %v", err)
	}
	if after.CAPEM == before.CAPEM {
		t.Fatal("rotate must mint a fresh CA certificate")
	}
}

func TestSignCSRIssuesDualEKUServingCert(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const agentID = "01890000-0000-7000-8000-000000000001"
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: agentID},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	certPEM, _, _, err := authority.SignCSR(csrPEM, agentID, time.Hour)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("issued cert is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued cert: %v", err)
	}

	if !slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Error("issued cert must keep ClientAuth EKU")
	}
	if !slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Error("issued cert must carry ServerAuth EKU (listen mode serves TLS)")
	}
	wantSAN := agenttransport.AgentServerName(agentID)
	if !slices.Contains(cert.DNSNames, wantSAN) {
		t.Errorf("issued cert DNSNames = %v, want to contain %q", cert.DNSNames, wantSAN)
	}
	if cert.Subject.CommonName != agentID {
		t.Errorf("CN = %q, want %q", cert.Subject.CommonName, agentID)
	}
}

// TestRecordDeniesPlaintextPersistByDefault pins the 3.1 belt-and-suspenders
// guard: certificateAuthority.record must refuse to return a persistable
// record when no encryption key is configured, unless the operator has
// explicitly set PANVEX_ALLOW_PLAINTEXT_CA. LoadBootstrap already fatally
// rejects an empty encryption key on the `serve` entrypoint; this guard
// protects any future non-serve caller (or misconfigured test/fixture) from
// silently writing the CA private key to disk/DB in plaintext.
func TestRecordDeniesPlaintextPersistByDefault(t *testing.T) {
	// TestMain opts the whole package into PANVEX_ALLOW_PLAINTEXT_CA=1 for
	// the other ~180 call sites in this package that don't care about CA
	// persistence semantics; unset it here so this test observes the real
	// default (unset/false) behaviour.
	t.Setenv(EnvAllowPlaintextCA, "")

	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	if _, err := authority.record(context.Background(), now, ""); err == nil {
		t.Fatal("record(empty key, no escape) error = nil, want ErrPlaintextCAPersistDenied")
	} else if !errors.Is(err, ErrPlaintextCAPersistDenied) {
		t.Fatalf("record(empty key, no escape) error = %v, want ErrPlaintextCAPersistDenied", err)
	}

	t.Setenv(EnvAllowPlaintextCA, "1")
	rec, err := authority.record(context.Background(), now, "")
	if err != nil {
		t.Fatalf("record(empty key, PANVEX_ALLOW_PLAINTEXT_CA=1) error = %v, want nil", err)
	}
	if rec.PrivateKeyPEM == "" {
		t.Fatal("record(empty key, escape set): PrivateKeyPEM is empty")
	}
}

// csrPEMWithKey builds a CERTIFICATE REQUEST PEM signed by the given key,
// for exercising checkCSRKeyStrength via issueAgentCertificateFromCSR.
func csrPEMWithKey(t *testing.T, agentID string, key crypto.Signer) string {
	t.Helper()
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: agentID},
	}, key)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
}

// TestIssueAgentCertificateFromCSRRejectsWeakRSAKey pins the 3.4 fix: a CSR
// carrying a 1024-bit RSA key must be rejected.
func TestIssueAgentCertificateFromCSRRejectsWeakRSAKey(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	weakKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey(1024): %v", err)
	}
	const agentID = "01890000-0000-7000-8000-000000000002"
	csrPEM := csrPEMWithKey(t, agentID, weakKey)

	_, err = authority.issueAgentCertificateFromCSR(csrPEM, agentID, time.Hour, false, now)
	if err == nil {
		t.Fatal("issueAgentCertificateFromCSR(1024-bit RSA) error = nil, want rejection")
	}
}

// TestIssueAgentCertificateFromCSRRejectsNonStandardCurve pins the 3.4 fix:
// a CSR carrying a P-224 ECDSA key (not P-256/P-384) must be rejected.
func TestIssueAgentCertificateFromCSRRejectsNonStandardCurve(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	p224Key, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(P224): %v", err)
	}
	const agentID = "01890000-0000-7000-8000-000000000003"
	csrPEM := csrPEMWithKey(t, agentID, p224Key)

	_, err = authority.issueAgentCertificateFromCSR(csrPEM, agentID, time.Hour, false, now)
	if err == nil {
		t.Fatal("issueAgentCertificateFromCSR(P-224) error = nil, want rejection")
	}
}

// TestIssueAgentCertificateFromCSRAcceptsP256Key confirms the normal agent
// key (ECDSA P-256, as generated by buildCSRPEM in cmd/agent) is unaffected
// by the 3.4 key-strength guard.
func TestIssueAgentCertificateFromCSRAcceptsP256Key(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	p256Key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(P256): %v", err)
	}
	const agentID = "01890000-0000-7000-8000-000000000004"
	csrPEM := csrPEMWithKey(t, agentID, p256Key)

	issued, err := authority.issueAgentCertificateFromCSR(csrPEM, agentID, time.Hour, false, now)
	if err != nil {
		t.Fatalf("issueAgentCertificateFromCSR(P-256) error = %v, want success", err)
	}
	if issued.CertificatePEM == "" {
		t.Fatal("issued.CertificatePEM is empty")
	}
}
