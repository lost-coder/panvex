// Package secretvault provides at-rest envelope encryption for sensitive
// fields (client secrets, TOTP secrets, etc.). It derives a single master
// key from the operator passphrase via Argon2id at startup, then derives
// per-domain AES-256-GCM keys via HKDF. Per-call encryption is cheap
// (one HKDF expand + one GCM seal) so the vault is safe to invoke on
// hot paths.
//
// Wire format: "PVS1:" || base64-raw(nonce || ciphertext). The domain
// label is bound as AAD so a value encrypted under one domain cannot be
// decrypted under another.
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

	"golang.org/x/crypto/argon2"
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
)

// AllDomains is the canonical list of registered domains. Callers
// should pass this to New to avoid reasoning about which subset is in
// use at any given site.
var AllDomains = []string{DomainClientSecret, DomainTOTP}

const (
	// Prefix marks vault-encrypted values. Anything without it is treated
	// as legacy plaintext; the vault returns it unchanged on decrypt so
	// existing rows keep working until they are next written.
	Prefix = "PVS1:"

	// masterSalt is a stable salt for the Argon2id master derivation.
	// The same passphrase deterministically produces the same master key
	// across restarts, so encrypted values remain decryptable.
	masterSalt = "panvex-secretvault-master-v1"

	// hkdfSalt namespaces per-domain key derivation under the master.
	hkdfSalt = "panvex-secretvault-domain-v1"

	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 2
	keyLen        = 32
)

// ErrPassphraseRequired is returned by Encrypt when the vault was built
// without a passphrase but a caller asked for an encrypted value.
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
	keys    map[string][]byte
}

// New constructs a vault from the operator passphrase. The list of
// domains must be exhaustive — passing an unknown domain to Encrypt or
// Decrypt later returns ErrUnknownDomain. Empty passphrase yields a
// disabled vault that passes values through.
func New(passphrase string, domains []string) (*Vault, error) {
	if strings.TrimSpace(passphrase) == "" {
		return &Vault{enabled: false}, nil
	}
	masterKey := argon2.IDKey([]byte(passphrase), []byte(masterSalt), argon2Time, argon2Memory, argon2Threads, keyLen)
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
		key := make([]byte, keyLen)
		reader := hkdf.New(sha256.New, masterKey, []byte(hkdfSalt), []byte(domain))
		if _, err := io.ReadFull(reader, key); err != nil {
			return nil, fmt.Errorf("secretvault: derive key for %q: %w", domain, err)
		}
		v.keys[domain] = key
	}
	return v, nil
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
// unchanged so callers can stay simple.
func (v *Vault) Encrypt(domain, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if v == nil || !v.enabled {
		return plaintext, nil
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
	return Prefix + base64.RawStdEncoding.EncodeToString(blob), nil
}

// Decrypt reverses Encrypt. Values without the vault prefix are
// returned unchanged so legacy rows keep working until they're next
// written. A prefixed value with an unconfigured vault is an error —
// the caller would otherwise see the raw ciphertext bytes.
func (v *Vault) Decrypt(domain, value string) (string, error) {
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	if v == nil || !v.enabled {
		return "", errors.New("secretvault: encrypted value present but vault is disabled")
	}
	key, ok := v.keys[domain]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownDomain, domain)
	}
	blob, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
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

// IsEncrypted reports whether the value carries the vault prefix. Used
// by migration tooling to decide whether to re-encrypt a row.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix)
}
