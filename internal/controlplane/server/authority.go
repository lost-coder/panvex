package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
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

	// PanelClientCN is the protocol-fixed CommonName of the control-plane's
	// outbound client certificate. Listen-mode agents verify the dialing peer's
	// leaf CN against this value; it is not operator-configurable.
	PanelClientCN = "control-plane.panvex.internal"
)

// EnvAllowPlaintextCA is a belt-and-suspenders escape hatch for
// certificateAuthority.record persisting the CA private key without
// encryption. The PRIMARY guard is settings.LoadBootstrap, which fatally
// rejects an empty/unset PANVEX_ENCRYPTION_KEY on the only production
// entrypoint (`serve`) — so this branch is normally unreachable in
// production. This second guard exists so a future non-`serve` entrypoint
// (or a test/dev fixture) cannot silently write a plaintext CA private key
// to disk/DB; it must opt in explicitly. Mirrors the
// PANVEX_ALLOW_DIRECT_EXPOSURE env-var idiom (strconv.ParseBool, default
// false, "1"/"true" truthy) in controlplane/server/trusted_proxy.go.
const EnvAllowPlaintextCA = "PANVEX_ALLOW_PLAINTEXT_CA"

// ErrPlaintextCAPersistDenied is returned by certificateAuthority.record
// when no encryption key is configured and PANVEX_ALLOW_PLAINTEXT_CA has
// not been explicitly set.
var ErrPlaintextCAPersistDenied = errors.New("refusing to persist CA private key in plaintext: no encryption key configured; set PANVEX_ENCRYPTION_KEY, or PANVEX_ALLOW_PLAINTEXT_CA=1 to explicitly allow plaintext persistence (dev/test only)")

// plaintextCAPersistAllowed reports whether the operator has explicitly
// opted into plaintext CA private key persistence via
// PANVEX_ALLOW_PLAINTEXT_CA. Read directly via os.Getenv, mirroring the
// PANVEX_ALLOW_DIRECT_EXPOSURE idiom: accepts "1"/"true" (case-insensitive)
// via strconv.ParseBool, defaulting to false.
func plaintextCAPersistAllowed() bool {
	v := strings.TrimSpace(os.Getenv(EnvAllowPlaintextCA))
	if v == "" {
		return false
	}
	allowed, err := strconv.ParseBool(v)
	return err == nil && allowed
}

type issuedCertificate struct {
	CertificatePEM string
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
	// clientCertificate is the panel's OUTBOUND identity (ClientAuth EKU,
	// CN=PanelClientCN). Presented when the panel dials listen-mode agents
	// and during the reverse bootstrap exchange. The chain includes the CA
	// DER so the agent's bootstrap SPKI-pin verifier finds the pinned cert
	// in the presented chain.
	clientCertificate tls.Certificate
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

// loadOrCreateCertificateAuthority resolves the panel CA: load the persisted
// record (aborting startup with an actionable error if expired), otherwise
// mint a new one and persist it.
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
// a stored CA record. Lifecycle: decrypt, parse, expiry check, opportunistic
// re-encryption (plaintext → ENC2:).
func loadExistingCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, record storage.CertificateAuthorityRecord, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	// Reject pre-release ENC:v1 blobs loudly regardless of whether an
	// encryption key is configured — decryptPEM only runs when encryptionKey
	// is set, so we must guard here to avoid silently treating the blob as
	// plaintext PEM (which would produce a confusing decode error).
	if isEncryptedPEM(record.PrivateKeyPEM) && !strings.HasPrefix(record.PrivateKeyPEM, encryptedPEMPrefixV2) {
		return nil, errors.New("CA private key: legacy ENC:v1 format is no longer supported (pre-release format removed); delete the stored certificate authority record and re-bootstrap, or restore an ENC2: backup")
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
		if err := reEncryptCertificateAuthority(ctx, store, authority, now, encryptionKey); err != nil {
			return nil, err
		}
	}
	return authority, nil
}

// handleCertificateAuthorityExpiry returns (true, nil, err) when the stored
// CA has expired — startup is aborted with an actionable error that names the
// recovery command. Silent regeneration would invalidate every enrolled agent
// without warning (audit 2026-06-09, A5). Use `rotate-ca --confirm` instead.
// Otherwise it logs the expiring-soon warning (when remaining <30d) and
// returns (false, nil, nil).
func handleCertificateAuthorityExpiry(_ context.Context, _ storage.CertificateAuthorityStore, authority *certificateAuthority, now time.Time, _ string) (bool, *certificateAuthority, error) {
	remaining := authority.certificate.NotAfter.Sub(now)
	if remaining <= 0 {
		return true, nil, fmt.Errorf(
			"control-plane CA certificate expired %s ago; refusing to start: regenerating would invalidate every enrolled agent. Run `panvex-control-plane rotate-ca --confirm` to mint a new CA (all agents must re-enroll afterwards)",
			(-remaining).Round(time.Hour),
		)
	}
	if remaining < 30*24*time.Hour {
		slog.Warn("control-plane CA certificate expiring soon", "remaining", remaining.Round(time.Hour).String())
	}
	return false, nil, nil
}

// reEncryptCertificateAuthority opportunistically re-encrypts a plaintext CA
// private key to ENC2: (Argon2id). Errors are non-fatal: they are logged as
// warnings so startup is not blocked by a transient store failure.
func reEncryptCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, authority *certificateAuthority, now time.Time, encryptionKey string) error {
	rec, recErr := authority.record(ctx, now, encryptionKey)
	if recErr != nil {
		slog.WarnContext(ctx, "control-plane CA private key re-encryption failed", "error", recErr)
		return nil
	}
	if putErr := store.PutCertificateAuthority(ctx, rec); putErr != nil {
		slog.WarnContext(ctx, "control-plane CA private key migration persist failed", "error", putErr)
		return nil
	}
	return nil
}

// RotateCertificateAuthority mints a fresh CA and overwrites the stored
// record. Used by the `rotate-ca` CLI subcommand — the only safe, operator-
// acknowledged path for CA replacement. Every enrolled agent must re-enroll
// after.
func RotateCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) error {
	if store == nil {
		return errors.New("rotate-ca requires a persistent store")
	}
	_, err := persistNewCertificateAuthority(ctx, store, now, encryptionKey)
	return err
}

// persistNewCertificateAuthority generates a fresh CA and stores it. Used by
// the bootstrap path (no record yet) and by RotateCertificateAuthority (the
// explicit rotate-ca subcommand). Expired CA records are no longer silently
// regenerated here; see handleCertificateAuthorityExpiry.
func persistNewCertificateAuthority(ctx context.Context, store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	authority, err := newCertificateAuthority(now)
	if err != nil {
		return nil, err
	}
	record, err := authority.record(ctx, now, encryptionKey)
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

	clientPair, err := issuePanelClientCertificate(certificate, privateKey, now)
	if err != nil {
		return nil, err
	}

	return &certificateAuthority{
		certificate:       certificate,
		privateKey:        privateKey,
		caPEM:             caPEM,
		serverCertificate: serverPair,
		clientCertificate: clientPair,
	}, nil
}

func (a *certificateAuthority) record(ctx context.Context, now time.Time, encryptionKey string) (storage.CertificateAuthorityRecord, error) {
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
	} else if !plaintextCAPersistAllowed() {
		// Belt-and-suspenders (audit 2026-07-01, M1): LoadBootstrap already
		// fatally rejects an empty PANVEX_ENCRYPTION_KEY on the `serve`
		// entrypoint, but record() must not itself trust that upstream
		// guard — a future non-serve caller (or misconfigured test) must
		// not be able to silently write the CA private key to disk/DB in
		// plaintext.
		return storage.CertificateAuthorityRecord{}, ErrPlaintextCAPersistDenied
	} else {
		slog.WarnContext(ctx, "persisting control-plane CA private key in plaintext: PANVEX_ALLOW_PLAINTEXT_CA is set",
			"alert", "plaintext_ca_persist_allowed")
	}

	return storage.CertificateAuthorityRecord{
		CAPEM:         a.caPEM,
		PrivateKeyPEM: keyPEM,
		UpdatedAt:     now.UTC(),
	}, nil
}

// checkCSRKeyStrength rejects agent CSR public keys that are too weak to
// trust for an issued leaf certificate (audit 2026-07-01, LOW: CSR weak-key).
// The agent itself always generates ECDSA P-256 keys (buildCSRPEM), so this
// exists to reject a malicious or buggy CSR presenting a deliberately weak
// key rather than to constrain the agent's own behaviour.
func checkCSRKeyStrength(pub any) error {
	switch pubKey := pub.(type) {
	case *rsa.PublicKey:
		if pubKey.N.BitLen() < 2048 {
			return fmt.Errorf("RSA key too small: %d bits, want >= 2048", pubKey.N.BitLen())
		}
		return nil
	case *ecdsa.PublicKey:
		if pubKey.Curve != elliptic.P256() && pubKey.Curve != elliptic.P384() {
			return fmt.Errorf("ECDSA curve %s not permitted, want P-256 or P-384", pubKey.Curve.Params().Name)
		}
		return nil
	case ed25519.PublicKey:
		// Ed25519 keys have a fixed, adequately strong security level;
		// no additional check needed. Not used by the agent today
		// (buildCSRPEM always emits ECDSA P-256), but nothing downstream
		// (x509.CreateCertificate signing an Ed25519 SPKI with an ECDSA
		// CA key) is incompatible with it.
		return nil
	default:
		return fmt.Errorf("unsupported CSR public key type %T", pub)
	}
}

// issueAgentCertificateFromCSR is the single issuance path for agent leaf
// certificates (A9): the agent generates the keypair, the panel signs the
// CSR. The certificate is dual-EKU (ClientAuth + ServerAuth) and carries
// the fixed DNS SAN AgentServerName(agentID) so it can serve the agent's
// gRPC listener in reverse transport mode and still authenticate as a
// client in dial mode (A1).
//
// requireCNMatch: renewal/recovery paths know the agent's identity from the
// presented credentials and must bind the CSR CN to it; the initial HTTP
// enrollment mints the agentID server-side AFTER the request arrives, so it
// passes false and the template CN (always agentID) wins regardless of the
// CSR subject.
func (a *certificateAuthority) issueAgentCertificateFromCSR(csrPEM, agentID string, validFor time.Duration, requireCNMatch bool, now time.Time) (issuedCertificate, error) {
	csrBlock, _ := pem.Decode([]byte(csrPEM))
	if csrBlock == nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: invalid PEM block for agent %s", agentID)
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: parse: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: signature check: %w", err)
	}
	if err := checkCSRKeyStrength(csr.PublicKey); err != nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: weak key: %w for agent %s", err, agentID)
	}
	if requireCNMatch && csr.Subject.CommonName != agentID {
		return issuedCertificate{}, fmt.Errorf("sign csr: CN mismatch: got %q, want %q", csr.Subject.CommonName, agentID)
	}

	serial, err := randomSerial()
	if err != nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: serial: %w", err)
	}

	expiresAt := now.Add(validFor)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   agentID,
			Organization: []string{"Panvex Agents"},
		},
		DNSNames:     []string{agenttransport.AgentServerName(agentID)},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		SubjectKeyId: serial.Bytes(),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.certificate, csr.PublicKey, a.privateKey)
	if err != nil {
		return issuedCertificate{}, fmt.Errorf("sign csr: create: %w", err)
	}
	return issuedCertificate{
		CertificatePEM: encodePEM("CERTIFICATE", der),
		CAPEM:          a.caPEM,
		ExpiresAt:      expiresAt,
		Serial:         serial.Text(16),
	}, nil
}

// SignCSR implements bootstrap.CertificateAuthority (reverse bootstrap +
// in-stream renewal). CN must match agentID on these paths.
func (a *certificateAuthority) SignCSR(csrPEM, agentID string, validFor time.Duration) (certPEM, caPEM string, expiresAt time.Time, err error) {
	issued, err := a.issueAgentCertificateFromCSR(csrPEM, agentID, validFor, true, time.Now())
	if err != nil {
		return "", "", time.Time{}, err
	}
	return issued.CertificatePEM, issued.CAPEM, issued.ExpiresAt, nil
}

// certOverlapMax caps how long the previous agent credential stays accepted
// after a rotation. The natural bound is the old certificate's own expiry —
// accepting it until then is no weaker than not having rotated at all — but a
// long-lived certificate would otherwise keep the window open for months, so it
// is clamped here.
const certOverlapMax = 7 * 24 * time.Hour

// rotateAgentCredential records a freshly issued certificate as the agent's
// current credential while keeping the PREVIOUS one accepted until it expires
// (bounded by certOverlapMax).
//
// The overlap exists because issuance and delivery are not atomic: the panel
// writes the new pin, then sends the certificate. If that exchange is
// interrupted — panel restart, stream drop — the agent still holds the old
// certificate, and a verifier that only knows the new one refuses it. An
// inbound agent can ask for another signature and heal itself; a listen-mode
// node cannot, and used to sit stranded until an operator issued a recovery
// grant (R11 / R-1).
//
// Best-effort, like the pin write it replaces: a failure is logged loudly but
// does not abort issuance — losing the in-flight credential exchange to a
// transient DB error would be worse, and the next renewal retries.
//
// presentedSerial is the serial the agent authenticated with on the connection
// carrying this issuance ("" when there is none — enrollment, recovery). It is
// threaded to RotateAgentCert so a renewal presented over the PREVIOUS
// credential during an open overlap window keeps that credential accepted
// instead of evicting it (R11-1: two consecutive lost RenewalResponses used to
// strand a listen-mode node on a certificate outside the accepted set).
func (s *Server) rotateAgentCredential(ctx context.Context, agentID, certPEM, presentedSerial string) {
	if s.store == nil {
		return
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		s.logger.WarnContext(ctx, "rotate agent credential: issued cert is not PEM", "agent_id", agentID)
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		s.logger.WarnContext(ctx, "rotate agent credential: parse issued cert failed", "agent_id", agentID, "error", err)
		return
	}
	pin := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

	if err := s.store.RotateAgentCert(ctx, agentID, cert.SerialNumber.Text(16), pin[:], s.certOverlapDeadline(agentID), presentedSerial); err != nil {
		s.obs.ObserveAgentCertPinPersistFailure()
		s.logger.WarnContext(ctx, "rotate agent credential failed", "agent_id", agentID, "error", err,
			"alert", "agent_cert_pin_persist_failed")
	}
}

// certOverlapDeadline is when the credential being replaced stops being
// accepted: its own expiry, clamped to certOverlapMax. An agent whose expiry we
// do not know (not in the live store) gets the clamp.
//
// A generous deadline cannot resurrect a dead certificate: TLS refuses an
// expired one at the handshake, long before the serial is looked at. This
// deadline bounds the pin BOOKKEEPING, so a stale previous serial cannot linger
// in the accepted set indefinitely.
func (s *Server) certOverlapDeadline(agentID string) time.Time {
	now := s.now().UTC()
	deadline := now.Add(certOverlapMax)
	if agent, ok := s.live.Get(agentID); ok && agent.CertExpiresAt != nil && agent.CertExpiresAt.Before(deadline) {
		deadline = agent.CertExpiresAt.UTC()
	}
	return deadline
}

// serverCertNotAfter parses the leaf of the panel's gRPC serving
// certificate and returns its NotAfter. tls.X509KeyPair does not retain
// Leaf, so this re-parses Certificate[0]; one small parse per metrics
// poll tick (5s) is negligible. Zero time on any malformed state.
func (ca *certificateAuthority) serverCertNotAfter() time.Time {
	if ca == nil || len(ca.serverCertificate.Certificate) == 0 {
		return time.Time{}
	}
	leaf, err := x509.ParseCertificate(ca.serverCertificate.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return leaf.NotAfter
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
			CommonName:   PanelClientCN,
			Organization: []string{"Panvex"},
		},
		DNSNames:    []string{"localhost", PanelClientCN},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(serverCertificateLifetime),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
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

// issuePanelClientCertificate mints the panel's outbound client identity.
// ClientAuth-only: this keypair must never be usable to impersonate the
// panel's gRPC SERVER endpoint.
func issuePanelClientCertificate(caCertificate *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time) (tls.Certificate, error) {
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
			CommonName:   PanelClientCN,
			Organization: []string{"Panvex"},
		},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(serverCertificateLifetime),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, privateKey.Public(), caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		// Leaf first, then the CA: the reverse-bootstrap verifier on the
		// agent pins the CA's SPKI and needs it present in the chain.
		Certificate: [][]byte{der, caCertificate.Raw},
		PrivateKey:  privateKey,
	}, nil
}

// outboundTLSConfig is the base config for panel-dials-agent connections:
// trust = panel CA only, identity = panel client cert. ServerName is set
// per-dial by the outbound supervisor (AgentServerName(agentID)).
func (a *certificateAuthority) outboundTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(a.caPEM))
	return &tls.Config{
		Certificates: []tls.Certificate{a.clientCertificate},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}
}

// caFingerprint returns the lower-hex SHA-256 fingerprint of cert.Raw. Used
// by Server.CAPINHex so agents can pin the panel CA on first connect.
func caFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// certPinReader adapts the server to agenttransport.CertPinReader. It exists so
// the outbound dial verifier sees the SAME accepted-credential set as the
// inbound connect path: the current pin, plus the previous one while a rotation
// overlap window is open (R11).
type certPinReader struct {
	server *Server
}

func (r certPinReader) AcceptedAgentCertPins(ctx context.Context, agentID string) ([][]byte, error) {
	pins, err := r.server.store.GetAgentCertPins(ctx, agentID)
	if err != nil {
		return nil, err
	}

	accepted := make([][]byte, 0, 2)
	if len(pins.SPKI) > 0 {
		accepted = append(accepted, pins.SPKI)
	}
	if len(pins.PrevSPKI) > 0 && pins.OverlapUntil != nil && r.server.now().UTC().Before(*pins.OverlapUntil) {
		accepted = append(accepted, pins.PrevSPKI)
	}
	return accepted, nil
}
