package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	certificateAuthorityLifetime = 5 * 365 * 24 * time.Hour
	serverCertificateLifetime    = 365 * 24 * time.Hour
	agentCertificateLifetime     = 30 * 24 * time.Hour

	// pemTypeECPrivateKey is the PEM block type for ECDSA private keys
	// (RFC 5915). Centralised so the cert-issuing helpers all encode
	// the same header (Sonar S1192).
	pemTypeECPrivateKey = "EC PRIVATE KEY"
)

type issuedCertificate struct {
	CertificatePEM string
	PrivateKeyPEM  string
	CAPEM          string
	ExpiresAt      time.Time
	// Serial is the hex-encoded big-endian certificate serial. Used by
	// Server to pin the issued cert against the agent record so an
	// older revoked cert (which still chains to the CA) cannot be
	// replayed at gRPC connect time (Q4.U-S-04).
	Serial string
}

type certificateAuthority struct {
	certificate       *x509.Certificate
	privateKey        *ecdsa.PrivateKey
	caPEM             string
	serverCertificate tls.Certificate
}

func newCertificateAuthority(now time.Time) (*certificateAuthority, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Panvex Control Plane Root CA",
			Organization: []string{"Panvex"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certificateAuthorityLifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, privateKey.Public(), privateKey)
	if err != nil {
		return nil, err
	}

	parsedCertificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	return buildCertificateAuthority(parsedCertificate, privateKey, encodePEM("CERTIFICATE", der), now)
}

// ErrLegacyEnc1RequiresKey is returned when the persisted CA private key is
// stored in the legacy "ENC:v1" format but no encryption passphrase is
// configured to migrate it. P2-SEC-05: legacy ENC:v1 uses SHA-256 without a
// salt and must not be silently accepted — the operator must either provide
// the encryption key so we can re-encrypt as "ENC2:" or remove the stored key
// to regenerate the CA.
var ErrLegacyEnc1RequiresKey = errors.New("legacy ENC:v1 key requires --encryption-key-file to migrate")

// storedPEMIsLegacyV1 reports whether the stored value uses the ENC:v1 prefix
// exactly (not the successor ENC2:).
func storedPEMIsLegacyV1(stored string) bool {
	return strings.HasPrefix(stored, encryptedPEMPrefix) && !strings.HasPrefix(stored, encryptedPEMPrefixV2)
}

// loadOrCreateCertificateAuthority resolves the panel CA: load the persisted
// record, regenerate if expired, otherwise mint a new one and persist it.
//
// ctx is the boot-time lifecycle context (s.serverCtx) so Close() during a
// wedged storage call aborts the goroutine instead of leaking it past
// shutdown (Plan 3 Task 3).
func loadOrCreateCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	if store == nil {
		return newCertificateAuthority(now)
	}

	record, err := store.GetCertificateAuthority(ctx)
	if err == nil {
		return loadExistingCertificateAuthority(ctx, store, record, now, encryptionKey)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	return persistNewCertificateAuthority(ctx, store, now, encryptionKey)
}

// loadExistingCertificateAuthority validates and (when needed) migrates
// a stored CA record. Lifecycle: legacy-ENC:v1 guard, decrypt, parse,
// expiry check, opportunistic re-encryption.
func loadExistingCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, record storage.CertificateAuthorityRecord, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	// P2-SEC-05: refuse to silently retain a legacy ENC:v1 blob without
	// an encryption key. The legacy derivation is SHA-256 with no salt,
	// so keeping it in place forever leaves the CA key weakly protected.
	// Either the operator supplies --encryption-key-file (and we migrate
	// in-place to ENC2:) or startup fails so the weakness is surfaced.
	legacyV1 := storedPEMIsLegacyV1(record.PrivateKeyPEM)
	if legacyV1 && encryptionKey == "" {
		return nil, ErrLegacyEnc1RequiresKey
	}

	if encryptionKey != "" {
		decrypted, decErr := decryptPEM(record.PrivateKeyPEM, encryptionKey)
		if decErr != nil {
			return nil, decErr
		}
		record.PrivateKeyPEM = decrypted
	}

	authority, err := certificateAuthorityFromRecord(record, now)
	if err != nil {
		return nil, err
	}

	if expired, regenAuth, regenErr := handleCertificateAuthorityExpiry(ctx, store, authority, now, encryptionKey); expired {
		return regenAuth, regenErr
	}

	if encryptionKey != "" && needsReEncryption(record.PrivateKeyPEM) {
		if err := reEncryptCertificateAuthority(ctx, store, authority, now, encryptionKey, legacyV1); err != nil {
			return nil, err
		}
	}
	return authority, nil
}

// handleCertificateAuthorityExpiry returns (true, regenerated, err) when
// the stored CA has expired so the caller short-circuits to a freshly
// regenerated authority. Otherwise it logs the expiring-soon warning
// (when remaining <30d) and returns (false, nil, nil).
func handleCertificateAuthorityExpiry(ctx context.Context, store storage.CertificateAuthorityStore, authority *certificateAuthority, now time.Time, encryptionKey string) (bool, *certificateAuthority, error) {
	remaining := authority.certificate.NotAfter.Sub(now)
	if remaining <= 0 {
		slog.Warn("control-plane CA certificate expired, regenerating", "expired_ago", (-remaining).String())
		regen, err := persistNewCertificateAuthority(ctx, store, now, encryptionKey)
		return true, regen, err
	}
	if remaining < 30*24*time.Hour {
		slog.Warn("control-plane CA certificate expiring soon", "remaining", remaining.Round(time.Hour).String())
	}
	return false, nil, nil
}

// reEncryptCertificateAuthority migrates a plaintext or ENC:v1 stored
// key to ENC2:. P2-SEC-05: legacy ENC:v1 migration is mandatory — any
// error is fatal; for plaintext or other cases we log but do not block
// startup.
func reEncryptCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, authority *certificateAuthority, now time.Time, encryptionKey string, legacyV1 bool) error {
	rec, recErr := authority.record(now, encryptionKey)
	if recErr != nil {
		if legacyV1 {
			return fmt.Errorf("auto-migrate legacy ENC:v1 CA private key: %w", recErr)
		}
		slog.Warn("control-plane CA private key re-encryption failed", "error", recErr)
		return nil
	}
	if putErr := store.PutCertificateAuthority(ctx, rec); putErr != nil {
		if legacyV1 {
			return fmt.Errorf("auto-migrate legacy ENC:v1 CA private key: %w", putErr)
		}
		slog.Warn("control-plane CA private key migration persist failed", "error", putErr)
		return nil
	}
	if legacyV1 {
		slog.Info("control-plane CA private key migrated from ENC:v1 to ENC2:")
	}
	return nil
}

// persistNewCertificateAuthority generates a fresh CA and stores it. Used by
// both the bootstrap path (no record yet) and the regeneration path (existing
// record expired or unrecoverable) — the body is identical, so there is one
// implementation. The two call sites read better with the shared name than
// with two trivial wrappers.
func persistNewCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	authority, err := newCertificateAuthority(now)
	if err != nil {
		return nil, err
	}
	record, err := authority.record(now, encryptionKey)
	if err != nil {
		return nil, err
	}
	if err := store.PutCertificateAuthority(ctx, record); err != nil {
		return nil, err
	}
	return authority, nil
}

func certificateAuthorityFromRecord(record storage.CertificateAuthorityRecord, now time.Time) (*certificateAuthority, error) {
	certificateBlock, _ := pem.Decode([]byte(record.CAPEM))
	if certificateBlock == nil {
		return nil, errors.New("failed to decode persisted control-plane CA certificate")
	}

	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, err
	}

	privateKeyBlock, _ := pem.Decode([]byte(record.PrivateKeyPEM))
	if privateKeyBlock == nil {
		return nil, errors.New("failed to decode persisted control-plane CA private key")
	}

	privateKey, err := parseAuthorityPrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	return buildCertificateAuthority(certificate, privateKey, record.CAPEM, now)
}

func parseAuthorityPrivateKey(encoded []byte) (*ecdsa.PrivateKey, error) {
	privateKey, err := x509.ParseECPrivateKey(encoded)
	if err == nil {
		return privateKey, nil
	}

	parsedKey, pkcs8Err := x509.ParsePKCS8PrivateKey(encoded)
	if pkcs8Err != nil {
		return nil, err
	}

	ecdsaKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("persisted control-plane CA private key must be ECDSA")
	}

	return ecdsaKey, nil
}

func buildCertificateAuthority(certificate *x509.Certificate, privateKey *ecdsa.PrivateKey, caPEM string, now time.Time) (*certificateAuthority, error) {
	serverPair, err := issueServerCertificate(certificate, privateKey, now)
	if err != nil {
		return nil, err
	}

	return &certificateAuthority{
		certificate:       certificate,
		privateKey:        privateKey,
		caPEM:             caPEM,
		serverCertificate: serverPair,
	}, nil
}

func (a *certificateAuthority) record(now time.Time, encryptionKey string) (storage.CertificateAuthorityRecord, error) {
	privateDER, err := x509.MarshalECPrivateKey(a.privateKey)
	if err != nil {
		return storage.CertificateAuthorityRecord{}, err
	}

	keyPEM := encodePEM(pemTypeECPrivateKey, privateDER)
	if encryptionKey != "" {
		encrypted, encErr := encryptPEM(keyPEM, encryptionKey)
		if encErr != nil {
			return storage.CertificateAuthorityRecord{}, encErr
		}
		keyPEM = encrypted
	}

	return storage.CertificateAuthorityRecord{
		CAPEM:         a.caPEM,
		PrivateKeyPEM: keyPEM,
		UpdatedAt:     now.UTC(),
	}, nil
}

func (a *certificateAuthority) issueClientCertificate(commonName string, now time.Time) (issuedCertificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return issuedCertificate{}, err
	}

	serial, err := randomSerial()
	if err != nil {
		return issuedCertificate{}, err
	}

	expiresAt := now.Add(agentCertificateLifetime)
	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Panvex Agents"},
		},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		SubjectKeyId: serial.Bytes(),
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, a.certificate, privateKey.Public(), a.privateKey)
	if err != nil {
		return issuedCertificate{}, err
	}

	privateDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return issuedCertificate{}, err
	}

	return issuedCertificate{
		CertificatePEM: encodePEM("CERTIFICATE", der),
		PrivateKeyPEM:  encodePEM(pemTypeECPrivateKey, privateDER),
		CAPEM:          a.caPEM,
		ExpiresAt:      expiresAt,
		Serial:         serial.Text(16),
	}, nil
}

// SignCSR implements bootstrap.CertificateAuthority. It parses csrPEM,
// validates that the request's CN matches agentID, signs a new certificate
// using the panel CA, and returns the issued cert PEM, CA cert PEM, and
// NotAfter. The issued cert is client-auth only so the agent can present it
// on the post-enrollment mTLS dial.
func (a *certificateAuthority) SignCSR(csrPEM, agentID string, validFor time.Duration) (certPEM, caPEM string, expiresAt time.Time, err error) {
	csrBlock, _ := pem.Decode([]byte(csrPEM))
	if csrBlock == nil {
		return "", "", time.Time{}, fmt.Errorf("sign csr: invalid PEM block for agent %s", agentID)
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("sign csr: parse: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("sign csr: signature check: %w", err)
	}
	if csr.Subject.CommonName != agentID {
		return "", "", time.Time{}, fmt.Errorf("sign csr: CN mismatch: got %q, want %q", csr.Subject.CommonName, agentID)
	}

	serial, err := randomSerial()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("sign csr: serial: %w", err)
	}

	now := time.Now()
	expiresAt = now.Add(validFor)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   agentID,
			Organization: []string{"Panvex Agents"},
		},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		SubjectKeyId: serial.Bytes(),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.certificate, csr.PublicKey, a.privateKey)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("sign csr: create: %w", err)
	}
	return encodePEM("CERTIFICATE", der), a.caPEM, expiresAt, nil
}

func encodePEM(blockType string, bytes []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  blockType,
		Bytes: bytes,
	}))
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func (a *certificateAuthority) serverTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(a.caPEM))

	return &tls.Config{
		Certificates: []tls.Certificate{a.serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
}

func issueServerCertificate(caCertificate *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time) (tls.Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "control-plane.panvex.internal",
			Organization: []string{"Panvex"},
		},
		DNSNames:     []string{"localhost", "control-plane.panvex.internal"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(serverCertificateLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, privateKey.Public(), caKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	privateDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.X509KeyPair(
		[]byte(encodePEM("CERTIFICATE", der)),
		[]byte(encodePEM(pemTypeECPrivateKey, privateDER)),
	)
}

// caFingerprint returns the lower-hex SHA-256 fingerprint of cert.Raw. Used
// by Server.CAPINHex so agents can pin the panel CA on first connect.
func caFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}
