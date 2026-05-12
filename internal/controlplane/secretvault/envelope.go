package secretvault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Envelope encryption (Wave 5.2).
//
// PVS3 splits the at-rest key into a KEK (derived from the passphrase,
// same Argon2id chain we already use) and per-domain DEKs (random
// 32-byte keys, AES-GCM-wrapped under the KEK and persisted in
// cp_secrets). The DEK is what actually seals row ciphertexts; the
// KEK only ever sees DEK plaintext. Rotation re-wraps the DEKs under
// a new KEK without touching the ciphertext columns.
//
// Wire format:
//
//	PVS3:v<N>:base64-raw(nonce || ciphertext)
//
// `v<N>` is the DEK version (small integer; the active version per
// domain is stored in cp_secrets). AAD on the seal is the domain
// label, preserving the cross-domain isolation property PVS2 already
// gave us. cp_secrets rows used by the envelope:
//
//	vault_dek_<domain>_active            — utf-8 integer N
//	vault_dek_<domain>_v<N>_wrapped      — nonce || AES-GCM(KEK, dek_plain, AAD=domain‖v<N>)
//	vault_kek_fp                          — SHA-256(KEK), safety check on subsequent boots

// Prefix3 marks values encrypted under an envelope DEK. New writes
// land here once the vault is initialised in envelope mode.
const Prefix3 = "PVS3:"

// cp_secrets key templates. These are storage-row labels, not secret
// values; gosec G101's hardcoded-credential heuristic trips on the
// "kek_fp" substring but the actual secret lives in the row's VALUE,
// not its KEY.
const (
	cpSecretKEKFingerprint = "vault_kek_fp" //nolint:gosec // storage key label, not a credential
	dekActiveKeyTemplate   = "vault_dek_%s_active"
	dekWrappedKeyTemplate  = "vault_dek_%s_v%d_wrapped"
	// dekVersion is the only DEK version this revision ships. Multi-
	// version DEK rotation is a documented follow-up; the wire format
	// already carries the version byte so it can be flipped without
	// another prefix break.
	dekVersion = 1
)

// CPSecretReader is the minimal storage seam used at vault boot.
// Implementations: storage.CPSecretStore (production) and an
// in-memory fixture (envelope_test.go). Kept as an interface so the
// vault package stays storage-backend-agnostic.
type CPSecretReader interface {
	GetCPSecret(ctx context.Context, key string) ([]byte, error)
	PutCPSecret(ctx context.Context, key string, value []byte) error
}

// TxCPSecretStore is the optional transactional capability RotateKEK
// requires for safe atomic rotation. Production sqlite/postgres
// adapters implement it via a Tx wrapper around the underlying
// *sql.DB. RotateKEK probes for it and refuses to run on stores
// that don't satisfy the interface (an in-memory fixture without
// transaction semantics would leave the operator with a half-rotated
// cp_secrets table on a mid-flight crash).
type TxCPSecretStore interface {
	CPSecretReader
	// WithCPSecretTx runs fn inside a single database transaction.
	// All PutCPSecret calls on the CPSecretReader passed to fn are
	// part of that transaction; the tx commits on fn returning nil
	// and rolls back on any error.
	WithCPSecretTx(ctx context.Context, fn func(tx CPSecretReader) error) error
}

// ErrNonTransactionalStore reports that the configured CPSecret
// store doesn't satisfy TxCPSecretStore. RotateKEK refuses to run
// against such a store because a mid-rotation crash would leave the
// cp_secrets rows half re-wrapped — neither old nor new passphrase
// could subsequently open the vault.
var ErrNonTransactionalStore = errors.New("secretvault: store does not support transactions; cannot safely rotate KEK")

// ErrEnvelopeWrongKEK reports that the passphrase passed at startup
// does not match the fingerprint persisted on first start. Catches
// the operator-typo case where the wrong PANVEX_ENCRYPTION_KEY would
// otherwise silently produce gibberish on first Decrypt.
var ErrEnvelopeWrongKEK = errors.New("secretvault: KEK fingerprint mismatch — wrong PANVEX_ENCRYPTION_KEY?")

// envelope holds the per-domain DEKs cached in memory. Populated by
// loadOrInitDEKs at boot and consulted by Encrypt/Decrypt when the
// Vault has envelope mode enabled.
type envelope struct {
	// activeVersion maps domain → version that new encrypts use.
	activeVersion map[string]int
	// deks maps "domain|version" → decrypted DEK bytes. Holds every
	// historical version so PVS3 ciphertexts emitted under an older
	// active DEK still open after a DEK rotation (a follow-up
	// feature; the format already supports it).
	deks map[string][]byte
}

func dekCacheKey(domain string, version int) string {
	return domain + "|v" + strconv.Itoa(version)
}

// NewWithEnvelope is the production constructor: same passphrase /
// salt chain as NewWithSalt, but additionally loads (or initialises)
// per-domain DEKs from the CPSecretReader. New writes land in PVS3
// format; PVS1/PVS2 reads stay supported as before.
//
// On first start with envelope mode, a DEK is minted per domain, its
// wrapped form persisted, and the KEK fingerprint stamped so a future
// boot can detect a typo'd passphrase before garbage Decrypts surface.
func NewWithEnvelope(ctx context.Context, passphrase string, domains []string, salt []byte, store CPSecretReader) (*Vault, error) {
	v, err := NewWithSalt(passphrase, domains, salt)
	if err != nil {
		return nil, err
	}
	if !v.enabled {
		// Disabled vault is a pass-through; envelope adds nothing.
		return v, nil
	}
	if store == nil {
		return nil, errors.New("secretvault: envelope mode requires a CPSecretReader")
	}
	kek := deriveKEK(passphrase)
	if err := verifyOrStoreKEKFingerprint(ctx, store, kek); err != nil {
		return nil, err
	}
	env, err := loadOrInitDEKs(ctx, store, kek, domains)
	if err != nil {
		return nil, err
	}
	v.envelope = env
	v.kek = kek
	return v, nil
}

// deriveKEK is the passphrase → key-encryption-key step. Matches the
// existing master derivation in NewWithSalt byte-for-byte so the KEK
// is identical to what PVS2 used as the master input.
func deriveKEK(passphrase string) []byte {
	return argon2.IDKey([]byte(passphrase), []byte(masterSalt), argon2Time, argon2Memory, argon2Threads, keyLen)
}

// verifyOrStoreKEKFingerprint plants SHA-256(KEK) into cp_secrets on
// first start and verifies subsequent starts match. Catches the
// "operator typed the wrong passphrase" case before any Decrypt call
// returns garbage.
func verifyOrStoreKEKFingerprint(ctx context.Context, store CPSecretReader, kek []byte) error {
	want := sha256Sum(kek)
	got, err := store.GetCPSecret(ctx, cpSecretKEKFingerprint)
	if err != nil {
		return fmt.Errorf("read kek fingerprint: %w", err)
	}
	if len(got) == 0 {
		// First boot under envelope mode — stamp it.
		if err := store.PutCPSecret(ctx, cpSecretKEKFingerprint, want); err != nil {
			return fmt.Errorf("store kek fingerprint: %w", err)
		}
		return nil
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrEnvelopeWrongKEK
	}
	return nil
}

// loadOrInitDEKs fetches each domain's active DEK from cp_secrets,
// generating + persisting a fresh one if absent (first-start path).
// All decrypted DEKs land in the returned envelope's cache so
// Encrypt/Decrypt avoid storage round-trips at request time.
func loadOrInitDEKs(ctx context.Context, store CPSecretReader, kek []byte, domains []string) (*envelope, error) {
	env := &envelope{
		activeVersion: make(map[string]int, len(domains)),
		deks:          make(map[string][]byte, len(domains)),
	}
	for _, domain := range domains {
		active, err := readActiveVersion(ctx, store, domain)
		if err != nil {
			return nil, err
		}
		if active == 0 {
			if err := mintFirstDEK(ctx, store, kek, domain, env); err != nil {
				return nil, err
			}
			continue
		}
		// Existing install — load every persisted version so PVS3
		// ciphertexts emitted under an older active version still
		// decrypt after a future DEK rotation (format reserves the
		// version byte; rotation impl is a follow-up).
		for v := 1; v <= active; v++ {
			wrapped, err := store.GetCPSecret(ctx, wrappedKey(domain, v))
			if err != nil {
				return nil, fmt.Errorf("read wrapped DEK %q v%d: %w", domain, v, err)
			}
			if len(wrapped) == 0 {
				continue // version gap (shouldn't happen, but tolerate)
			}
			dek, err := unwrapDEK(kek, wrapped, domain, v)
			if err != nil {
				return nil, fmt.Errorf("unwrap DEK %q v%d: %w", domain, v, err)
			}
			env.deks[dekCacheKey(domain, v)] = dek
		}
		env.activeVersion[domain] = active
	}
	return env, nil
}

func mintFirstDEK(ctx context.Context, store CPSecretReader, kek []byte, domain string, env *envelope) error {
	dek := make([]byte, keyLen)
	if _, err := rand.Read(dek); err != nil {
		return fmt.Errorf("generate DEK for %q: %w", domain, err)
	}
	wrapped, err := wrapDEK(kek, dek, domain, dekVersion)
	if err != nil {
		return fmt.Errorf("wrap DEK for %q: %w", domain, err)
	}
	if err := store.PutCPSecret(ctx, wrappedKey(domain, dekVersion), wrapped); err != nil {
		return fmt.Errorf("persist wrapped DEK for %q: %w", domain, err)
	}
	if err := store.PutCPSecret(ctx, activeKey(domain), []byte(strconv.Itoa(dekVersion))); err != nil {
		return fmt.Errorf("persist active DEK version for %q: %w", domain, err)
	}
	env.activeVersion[domain] = dekVersion
	env.deks[dekCacheKey(domain, dekVersion)] = dek
	return nil
}

func readActiveVersion(ctx context.Context, store CPSecretReader, domain string) (int, error) {
	raw, err := store.GetCPSecret(ctx, activeKey(domain))
	if err != nil {
		return 0, fmt.Errorf("read active DEK version for %q: %w", domain, err)
	}
	if len(raw) == 0 {
		return 0, nil
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil {
		return 0, fmt.Errorf("parse active DEK version for %q: %w", domain, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("active DEK version for %q is %d (must be >= 1)", domain, n)
	}
	return n, nil
}

func activeKey(domain string) string {
	return fmt.Sprintf(dekActiveKeyTemplate, domain)
}

func wrappedKey(domain string, version int) string {
	return fmt.Sprintf(dekWrappedKeyTemplate, domain, version)
}

// wrapDEK seals a plaintext DEK under the KEK. AAD binds the wrap to
// (domain, version) so a wrapped DEK row can't be relocated to a
// different domain or version without detection.
func wrapDEK(kek, dek []byte, domain string, version int) ([]byte, error) {
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := wrapAAD(domain, version)
	out := make([]byte, 0, len(nonce)+gcm.Overhead()+len(dek))
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, dek, aad)
	return out, nil
}

// unwrapDEK reverses wrapDEK. Returns ErrCorrupted on any AEAD
// failure (wrong KEK, truncated bytes, tampered AAD).
func unwrapDEK(kek, wrapped []byte, domain string, version int) ([]byte, error) {
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	if len(wrapped) < gcm.NonceSize() {
		return nil, ErrCorrupted
	}
	nonce, ct := wrapped[:gcm.NonceSize()], wrapped[gcm.NonceSize():]
	dek, err := gcm.Open(nil, nonce, ct, wrapAAD(domain, version))
	if err != nil {
		return nil, fmt.Errorf("%w: unwrap: %w", ErrCorrupted, err)
	}
	return dek, nil
}

func wrapAAD(domain string, version int) []byte {
	return []byte(domain + "|v" + strconv.Itoa(version))
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// encryptEnvelope produces a PVS3 ciphertext using the active DEK for
// the domain. AAD = domain (so cross-domain open still fails).
func (v *Vault) encryptEnvelope(domain, plaintext string) (string, error) {
	version, dek, ok := v.activeDEK(domain)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownDomain, domain)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	blob := make([]byte, 0, len(nonce)+gcm.Overhead()+len(plaintext))
	blob = append(blob, nonce...)
	blob = gcm.Seal(blob, nonce, []byte(plaintext), []byte(domain))
	return fmt.Sprintf("%sv%d:%s", Prefix3, version, base64.RawStdEncoding.EncodeToString(blob)), nil
}

// decryptEnvelope reverses encryptEnvelope. body is the tail after
// "PVS3:".
func (v *Vault) decryptEnvelope(domain, body string) (string, error) {
	if v.envelope == nil {
		return "", errors.New("secretvault: PVS3 ciphertext present but envelope not initialised")
	}
	colon := strings.IndexByte(body, ':')
	if colon < 2 || body[0] != 'v' {
		return "", fmt.Errorf("%w: bad PVS3 version prefix", ErrCorrupted)
	}
	// Cap the parsed width before strconv.Atoi to bound parsing time
	// + log-pollution from attacker-supplied ciphertexts. Real
	// rotations bump dekVersion by one each time; 4 digits leaves
	// orders-of-magnitude headroom over any sane rotation cadence.
	if colon-1 > 4 {
		return "", fmt.Errorf("%w: PVS3 version too wide", ErrCorrupted)
	}
	version, err := strconv.Atoi(body[1:colon])
	if err != nil || version < 1 {
		return "", fmt.Errorf("%w: parse PVS3 version: %w", ErrCorrupted, err)
	}
	dek, ok := v.envelope.deks[dekCacheKey(domain, version)]
	if !ok {
		return "", fmt.Errorf("%w: domain %q v%d", ErrUnknownDomain, domain, version)
	}
	blob, err := base64.RawStdEncoding.DecodeString(body[colon+1:])
	if err != nil {
		return "", fmt.Errorf("%w: base64: %w", ErrCorrupted, err)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize() {
		return "", ErrCorrupted
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, []byte(domain))
	if err != nil {
		return "", fmt.Errorf("%w: open: %w", ErrCorrupted, err)
	}
	return string(plain), nil
}

// activeDEK returns the active DEK for the domain plus its version.
// Used by Encrypt to decide which DEK to seal under.
func (v *Vault) activeDEK(domain string) (int, []byte, bool) {
	if v.envelope == nil {
		return 0, nil, false
	}
	version, ok := v.envelope.activeVersion[domain]
	if !ok {
		return 0, nil, false
	}
	dek, ok := v.envelope.deks[dekCacheKey(domain, version)]
	if !ok {
		return 0, nil, false
	}
	return version, dek, true
}

// EnvelopeEnabled reports whether this vault has DEKs loaded. False
// means Encrypt still uses PVS2; true means PVS3 for new writes.
// Used by tests and the rotate-encryption-key subcommand.
func (v *Vault) EnvelopeEnabled() bool {
	return v != nil && v.envelope != nil
}

// RotateKEK re-wraps every persisted DEK under newPassphrase atomically.
// The store MUST implement TxCPSecretStore — every cp_secrets write
// (re-wrapped DEKs + new fingerprint) lands in a single database
// transaction, so a mid-rotation crash never leaves the operator
// locked out by half-rotated rows.
//
// On success the vault's in-memory KEK is replaced and the OLD KEK
// bytes are wiped so a heap dump from this process can't recover
// them. Returns the number of DEKs re-wrapped (one per
// (domain, version) pair persisted).
func (v *Vault) RotateKEK(ctx context.Context, store CPSecretReader, newPassphrase string) (int, error) {
	if v == nil || !v.enabled {
		return 0, errors.New("secretvault: cannot rotate KEK on a disabled vault")
	}
	if v.envelope == nil {
		return 0, errors.New("secretvault: cannot rotate KEK without envelope mode (boot the vault with NewWithEnvelope first)")
	}
	if strings.TrimSpace(newPassphrase) == "" {
		return 0, ErrPassphraseRequired
	}
	txStore, ok := store.(TxCPSecretStore)
	if !ok {
		return 0, ErrNonTransactionalStore
	}
	newKEK := deriveKEK(newPassphrase)
	if subtle.ConstantTimeCompare(newKEK, v.kek) == 1 {
		zero(newKEK)
		return 0, errors.New("secretvault: new passphrase is identical to current — refusing no-op rotation")
	}

	// Pre-compute every re-wrapped DEK in memory so the transaction
	// body is pure I/O without crypto failure modes. A wrap error
	// here aborts the rotation before any cp_secrets row is touched.
	type wrappedRow struct {
		key   string
		bytes []byte
	}
	rows := make([]wrappedRow, 0, len(v.envelope.deks))
	for cacheKey, dek := range v.envelope.deks {
		domain, version, err := parseDEKCacheKey(cacheKey)
		if err != nil {
			zero(newKEK)
			return 0, err
		}
		wrapped, err := wrapDEK(newKEK, dek, domain, version)
		if err != nil {
			zero(newKEK)
			return 0, fmt.Errorf("re-wrap DEK %s v%d: %w", domain, version, err)
		}
		rows = append(rows, wrappedRow{key: wrappedKey(domain, version), bytes: wrapped})
	}
	newFP := sha256Sum(newKEK)

	rewrapped := 0
	if err := txStore.WithCPSecretTx(ctx, func(tx CPSecretReader) error {
		for _, r := range rows {
			if err := tx.PutCPSecret(ctx, r.key, r.bytes); err != nil {
				return fmt.Errorf("persist re-wrapped DEK %s: %w", r.key, err)
			}
			rewrapped++
		}
		if err := tx.PutCPSecret(ctx, cpSecretKEKFingerprint, newFP); err != nil {
			return fmt.Errorf("persist new KEK fingerprint: %w", err)
		}
		return nil
	}); err != nil {
		zero(newKEK)
		return 0, err
	}

	// Atomic commit succeeded — flip in-memory state and wipe the
	// old KEK so a heap dump can't recover it.
	oldKEK := v.kek
	v.kek = newKEK
	zero(oldKEK)
	return rewrapped, nil
}

// zero wipes a byte slice in place. The defensive wipe means a heap
// dump (or swap page) from a panel running under the new KEK can't
// recover the previously-loaded one. Not a substitute for OS-level
// mlock; this is best-effort defence-in-depth.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// parseDEKCacheKey reverses dekCacheKey. Format: "domain|vN".
func parseDEKCacheKey(s string) (string, int, error) {
	pipe := strings.IndexByte(s, '|')
	if pipe < 1 {
		return "", 0, fmt.Errorf("invalid DEK cache key %q", s)
	}
	domain := s[:pipe]
	rest := s[pipe+1:]
	if len(rest) < 2 || rest[0] != 'v' {
		return "", 0, fmt.Errorf("invalid DEK cache key version %q", s)
	}
	version, err := strconv.Atoi(rest[1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid DEK cache key version %q: %w", s, err)
	}
	return domain, version, nil
}
