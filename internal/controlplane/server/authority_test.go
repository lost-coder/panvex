package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
// encrypted with the legacy "ENC:v1" (SHA-256, no salt) derivation. The CA
// certificate itself is a freshly-generated P-256 ECDSA root with a
// long-enough validity that loadOrCreateCertificateAuthority does not
// regenerate it during the test.
func seedLegacyEnc1CA(t *testing.T, store storage.CertificateAuthorityStore, passphrase string, now time.Time) {
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
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}

	keyPEM := encodePEM("EC PRIVATE KEY", privDER)
	legacy, err := encryptPEMWithKey(keyPEM, deriveKeyV1(passphrase), encryptedPEMPrefix)
	if err != nil {
		t.Fatalf("encryptPEMWithKey: %v", err)
	}
	if !strings.HasPrefix(legacy, encryptedPEMPrefix) || strings.HasPrefix(legacy, encryptedPEMPrefixV2) {
		prefixLen := 10
		if len(legacy) < prefixLen {
			prefixLen = len(legacy)
		}
		t.Fatalf("seed payload must be ENC:v1, got prefix %q", legacy[:prefixLen])
	}

	caPEM := encodePEM("CERTIFICATE", der)
	if err := store.PutCertificateAuthority(context.Background(), storage.CertificateAuthorityRecord{
		CAPEM:         caPEM,
		PrivateKeyPEM: legacy,
		UpdatedAt:     now.UTC(),
	}); err != nil {
		t.Fatalf("PutCertificateAuthority: %v", err)
	}
}

// TestLegacyEnc1BlobAutoMigrates seeds a legacy ENC:v1 blob, then loads the
// authority with an encryption key configured. The stored blob must be
// rewritten as ENC2: in place — never retained as ENC:v1 when a key is
// available. (P2-SEC-05)
func TestLegacyEnc1BlobAutoMigrates(t *testing.T) {
	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	passphrase := "test-migration-passphrase"
	seedLegacyEnc1CA(t, store, passphrase, now)

	// Sanity: the seeded blob is ENC:v1 before migration.
	before, err := store.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("GetCertificateAuthority before: %v", err)
	}
	if !strings.HasPrefix(before.PrivateKeyPEM, encryptedPEMPrefix) ||
		strings.HasPrefix(before.PrivateKeyPEM, encryptedPEMPrefixV2) {
		t.Fatalf("seeded blob prefix must be ENC:v1, got %q", before.PrivateKeyPEM[:10])
	}

	// Run the load path with the matching encryption key configured.
	authority, err := loadOrCreateCertificateAuthority(context.Background(), store, now, passphrase)
	if err != nil {
		t.Fatalf("loadOrCreateCertificateAuthority: %v", err)
	}
	if authority == nil || authority.certificate == nil {
		t.Fatal("loadOrCreateCertificateAuthority returned nil authority")
	}

	// After startup the stored blob must be ENC2:, not ENC:v1.
	after, err := store.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("GetCertificateAuthority after: %v", err)
	}
	if !strings.HasPrefix(after.PrivateKeyPEM, encryptedPEMPrefixV2) {
		t.Fatalf("post-migrate blob prefix = %q, want %s", after.PrivateKeyPEM[:10], encryptedPEMPrefixV2)
	}

	// The re-encrypted value must still decrypt with the same passphrase
	// and yield the original PEM.
	decrypted, err := decryptPEM(after.PrivateKeyPEM, passphrase)
	if err != nil {
		t.Fatalf("decryptPEM(migrated): %v", err)
	}
	if !strings.Contains(decrypted, "EC PRIVATE KEY") {
		t.Fatalf("migrated plaintext does not contain PEM header: %q", decrypted)
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

// TestLegacyEnc1WithoutKeyFailsFast verifies that a legacy ENC:v1 blob with
// no --encryption-key-file configured produces a fatal startup error instead
// of silently continuing with the weaker derivation. (P2-SEC-05)
func TestLegacyEnc1WithoutKeyFailsFast(t *testing.T) {
	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	seedLegacyEnc1CA(t, store, "doesnt-matter", now)

	_, err = loadOrCreateCertificateAuthority(context.Background(), store, now, "")
	if err == nil {
		t.Fatal("loadOrCreateCertificateAuthority(ENC:v1, no key) error = nil, want fatal")
	}
	if !errors.Is(err, ErrLegacyEnc1RequiresKey) {
		t.Fatalf("error = %v, want ErrLegacyEnc1RequiresKey", err)
	}

	// The stored blob must remain untouched (we must not silently delete it).
	record, err := store.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("GetCertificateAuthority after fail: %v", err)
	}
	if !strings.HasPrefix(record.PrivateKeyPEM, encryptedPEMPrefix) ||
		strings.HasPrefix(record.PrivateKeyPEM, encryptedPEMPrefixV2) {
		t.Fatalf("blob prefix after failed load = %q, want untouched ENC:v1", record.PrivateKeyPEM[:10])
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
