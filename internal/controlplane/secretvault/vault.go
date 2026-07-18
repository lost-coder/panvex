// Package secretvault provides at-rest envelope encryption for sensitive
// fields (client secrets, TOTP secrets, etc.). It derives a single master
// key from the operator passphrase via Argon2id at startup, then derives
// per-domain AES-256-GCM keys via HKDF. Per-call encryption is cheap
// (one HKDF expand + one GCM seal) so the vault is safe to invoke on
// hot paths.
//
// Wire format: "PVSn:" || base64-raw(nonce || ciphertext). The domain
// label is bound as AAD so a value encrypted under one domain cannot be
// decrypted under another.
//
//   - PVS2: HKDF-derived per-domain key. The HKDF salt is per-install —
//     generated at first start and persisted in cp_secrets — so two
//     installations sharing a master passphrase do not derive identical
//     per-domain keys.
//   - PVS3: envelope format (per-domain DEK wrapped under a KEK); see
//     envelope.go. Produced by Encrypt once DEKs are loaded.
package secretvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"golang.org/x/crypto/hkdf"
)

// Domain identifiers used across the codebase. Keep these in sync with
// the slice passed to New so callers don't hit ErrUnknownDomain.
const (
	// DomainClientSecret is the per-client MTProto proxy secret stored
	// in clients.secret_ciphertext.
	DomainClientSecret = "client_secret"
	// DomainTOTP is the per-user TOTP shared secret stored in
	// users.totp_secret.
	DomainTOTP = "totp_secret"
	// DomainWebhookSecret is the per-endpoint HMAC key stored in
	// webhook_endpoints.secret_ciphertext. Used by the webhook
	// outbox worker to sign each delivery body.
	DomainWebhookSecret = "webhook_secret"
	// DomainIntegrationConfig protects the credential bundle stored in
	// integration_providers.config (e.g. a Cloudflare API token). The
	// whole JSON config blob is sealed under this domain at rest.
	DomainIntegrationConfig = "integration_config"
)

// AllDomains is the canonical list of registered domains. Callers
// should pass this to New to avoid reasoning about which subset is in
// use at any given site.
var AllDomains = []string{DomainClientSecret, DomainTOTP, DomainWebhookSecret, DomainIntegrationConfig}

const (
	// Prefix2 marks values encrypted under the per-install HKDF salt.
	// Current default for every Encrypt call.
	Prefix2 = "PVS2:"

	// Prefix is the current encrypt prefix. Exposed for tests; callers
	// outside this package should rely on Encrypt/Decrypt.
	Prefix = Prefix2

	// masterSalt is a stable salt for the Argon2id master derivation.
	// The same passphrase deterministically produces the same master key
	// across restarts, so encrypted values remain decryptable.
	masterSalt = "panvex-secretvault-master-v1"

	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 2
	keyLen        = 32

	// HKDFSaltBytes is the size of the per-install HKDF salt persisted
	// in cp_secrets. 32 bytes matches the SHA-256 output the HKDF
	// extract step would otherwise need to seed.
	HKDFSaltBytes = 32
)

// ErrPassphraseRequired reports that an operation needing the master
// passphrase (envelope DEK load / KEK rotation) ran on a vault built
// without one. Encrypt itself never returns it: a passphrase-less vault
// is a documented pass-through.
var ErrPassphraseRequired = errors.New("secretvault: passphrase not configured")

// ErrUnknownDomain is returned when Encrypt/Decrypt receive a domain
// that was not registered at vault construction.
var ErrUnknownDomain = errors.New("secretvault: unknown domain")

// ErrCorrupted indicates a stored value carries the vault prefix but
// cannot be parsed or authenticated.
var ErrCorrupted = errors.New("secretvault: ciphertext corrupted or wrong key")

// Vault encrypts and decrypts sensitive field values.
//
// A nil or zero Vault is a valid pass-through: Encrypt returns the
// plaintext unchanged, Decrypt returns the value unchanged. This keeps
// callers that don't have access to a key (tests, dev with no key set)
// working without conditionals at every callsite.
type Vault struct {
	enabled bool
	// keys holds the active per-domain keys derived under the
	// per-install salt; used for both PVS2 encrypt and PVS2 decrypt.
	keys map[string][]byte
	// envelope, when non-nil, switches Encrypt to PVS3 (per-domain
	// DEKs wrapped under the KEK; see envelope.go). Decrypt continues
	// to handle PVS2 via keys and dispatches PVS3 to the envelope path.
	envelope *envelope
	// kek is the current key-encryption-key. Held in memory so
	// RotateKEK can replace it atomically with the new derivation
	// after re-wrapping every persisted DEK.
	kek []byte
}

// NewWithSalt constructs a vault from the operator passphrase using the
// supplied per-install HKDF salt. The list of domains must be
// exhaustive — passing an unknown domain to Encrypt or Decrypt later
// returns ErrUnknownDomain. Empty passphrase yields a disabled vault
// that passes values through.
func NewWithSalt(passphrase string, domains []string, salt []byte) (*Vault, error) {
	if strings.TrimSpace(passphrase) == "" {
		return &Vault{enabled: false}, nil
	}
	if len(salt) == 0 {
		return nil, errors.New("secretvault: empty per-install salt")
	}
	// Through kdf.IDKey, not argon2 directly: the panel serialises every
	// Argon2id derivation and collects each block array before the next one
	// allocates. This call runs at startup beside the CA-key decrypt and the
	// admin bootstrap hash, so an ungated 64 MiB buffer here stacks on top of
	// theirs. The parameters stay hardcoded (NOT profile-driven): the master
	// key must reproduce byte-for-byte across restarts or every stored secret
	// becomes undecryptable.
	masterKey := kdf.IDKey([]byte(passphrase), []byte(masterSalt), argon2Time, argon2Memory, argon2Threads, keyLen)
	v := &Vault{
		enabled: true,
		keys:    make(map[string][]byte, len(domains)),
	}
	for _, domain := range domains {
		if strings.TrimSpace(domain) == "" {
			return nil, errors.New("secretvault: empty domain")
		}
		if _, exists := v.keys[domain]; exists {
			return nil, fmt.Errorf("secretvault: duplicate domain %q", domain)
		}
		key, err := deriveDomainKey(masterKey, salt, domain)
		if err != nil {
			return nil, fmt.Errorf("secretvault: derive key for %q: %w", domain, err)
		}
		v.keys[domain] = key
	}
	return v, nil
}

// deriveDomainKey produces a 32-byte AES-GCM key for the given domain
// from the master key, using the supplied HKDF salt. info=domain so the
// same (master, salt) deterministically yields a distinct key per
// domain.
func deriveDomainKey(masterKey, salt []byte, domain string) ([]byte, error) {
	key := make([]byte, keyLen)
	reader := hkdf.New(sha256.New, masterKey, salt, []byte(domain))
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// Enabled reports whether the vault has a passphrase configured. When
// false, Encrypt is a no-op and Decrypt accepts plaintext as-is.
func (v *Vault) Enabled() bool {
	if v == nil {
		return false
	}
	return v.enabled
}

// Encrypt seals the plaintext under the given domain key. Empty
// plaintext is returned as-is (no point storing ciphertext for an
// empty secret). When the vault is disabled the plaintext is returned
// unchanged so callers can stay simple. New ciphertexts target the
// envelope path (PVS3) once DEKs are loaded; the legacy PVS2 path is
// the fallback for bare NewWithSalt vaults (tests / dev).
func (v *Vault) Encrypt(domain, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if v == nil || !v.enabled {
		return plaintext, nil
	}
	if v.envelope != nil {
		return v.encryptEnvelope(domain, plaintext)
	}
	key, ok := v.keys[domain]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownDomain, domain)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// Pre-size so append never reallocates and gocritic does not flag
	// the "append result not assigned to the same slice" anti-pattern.
	blob := make([]byte, 0, len(nonce)+gcm.Overhead()+len(plaintext))
	blob = append(blob, nonce...)
	blob = gcm.Seal(blob, nonce, []byte(plaintext), []byte(domain))
	return Prefix2 + base64.RawStdEncoding.EncodeToString(blob), nil
}

// Decrypt reverses Encrypt. Values without any vault prefix are
// returned unchanged so plaintext rows keep working until they're next
// written. PVS3 routes through the envelope (per-domain DEK); PVS2 uses
// the per-install HKDF-derived key. A prefixed value with an
// unconfigured vault is an error — the caller would otherwise see the
// raw ciphertext bytes.
func (v *Vault) Decrypt(domain, value string) (string, error) {
	switch {
	case strings.HasPrefix(value, Prefix3):
		return v.decryptEnvelope(domain, strings.TrimPrefix(value, Prefix3))
	case strings.HasPrefix(value, Prefix2):
		return v.decryptWithKey(domain, strings.TrimPrefix(value, Prefix2))
	case hasVaultPrefix(value):
		// Carries a "PVSn:" marker we no longer recognise (e.g. a legacy
		// PVS1 row whose decrypt path was dropped). Returning it verbatim
		// would ship raw ciphertext to Telemt / use it as a TOTP secret —
		// a silent corruption. Fail loudly instead so the operator sees it.
		return "", fmt.Errorf("%w: unrecognised vault format", ErrCorrupted)
	default:
		return value, nil
	}
}

// hasVaultPrefix reports whether value carries a "PVS<digits>:" marker,
// i.e. it was produced by some version of this vault even if the current
// build cannot decrypt that particular format. Genuine plaintext rows
// (which the vault passes through unchanged) never match this shape.
func hasVaultPrefix(value string) bool {
	rest, ok := strings.CutPrefix(value, "PVS")
	if !ok {
		return false
	}
	digits := 0
	for i := 0; i < len(rest); i++ {
		if rest[i] >= '0' && rest[i] <= '9' {
			digits++
			continue
		}
		return rest[i] == ':' && digits > 0
	}
	return false
}

// decryptWithKey performs the AES-GCM open under the active per-domain
// key (PVS2).
func (v *Vault) decryptWithKey(domain, body string) (string, error) {
	if v == nil || !v.enabled {
		return "", errors.New("secretvault: encrypted value present but vault is disabled")
	}
	key, ok := v.keys[domain]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownDomain, domain)
	}
	blob, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		return "", fmt.Errorf("%w: base64: %w", ErrCorrupted, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize() {
		return "", ErrCorrupted
	}
	nonce, ciphertext := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(domain))
	if err != nil {
		return "", fmt.Errorf("%w: open: %w", ErrCorrupted, err)
	}
	return string(plaintext), nil
}

// EncryptIfEnabled is the encrypt idiom every domain needs: a nil or disabled
// vault passes the value through (the no-key mode), an empty value is a no-op,
// and a value that ALREADY carries a vault prefix is returned unchanged.
//
// That last guard is not cosmetic. Encrypting a ciphertext again double-wraps
// it and corrupts the row — the failure that once shipped a literal "PVS2:..."
// string to Telemt as a client secret. Callers must not hand-roll this.
func (v *Vault) EncryptIfEnabled(domain, plaintext string) (string, error) {
	if v == nil || !v.Enabled() || plaintext == "" || IsEncrypted(plaintext) {
		return plaintext, nil
	}
	return v.Encrypt(domain, plaintext)
}

// IsEncrypted reports whether the value carries any vault prefix. Used
// by migration tooling to decide whether to re-encrypt a row.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix2) || strings.HasPrefix(value, Prefix3)
}
