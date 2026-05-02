package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

var (
	// ErrTotpRequired reports a missing second factor for a TOTP-enabled account.
	ErrTotpRequired = errors.New("totp code required")
	// ErrInvalidTotpCode reports a second factor mismatch.
	ErrInvalidTotpCode = errors.New("invalid totp code")
	// ErrTotpSetupNotFound reports a missing or expired pending TOTP setup.
	ErrTotpSetupNotFound = errors.New("totp setup not found")
)

const pendingTotpSetupTTL = 10 * time.Minute

// totpUseKey identifies a consumed TOTP code for replay prevention.
type totpUseKey struct {
	UserID string
	Code   string
}

type pendingTotpSetup struct {
	Secret    string
	CreatedAt time.Time
}

// SetConsumedTotpStore wires the persistent replay-prevention store
// (Q2.U-S-17). Once set, every consumed TOTP code is mirrored to the
// store and the in-memory map is rebuilt from it on RestoreSessions.
func (s *Service) SetConsumedTotpStore(store storage.ConsumedTotpStore) {
	s.mu.Lock()
	s.consumedTotpStore = store
	s.mu.Unlock()
}

// persistConsumedTotpAsync mirrors a freshly consumed (user_id, code)
// pair to the configured store. Best-effort: a store error is logged
// but does not fail the auth flow that triggered the consume.
func (s *Service) persistConsumedTotp(ctx context.Context, userID, code string, usedAt time.Time) {
	store := s.consumedTotpStore
	if store == nil {
		return
	}
	if err := store.UpsertConsumedTotp(ctx, storage.ConsumedTotpRecord{
		UserID: userID,
		Code:   code,
		UsedAt: usedAt.UTC(),
	}); err != nil {
		slog.Warn("auth: persist consumed TOTP failed", "user_id", userID, "error", err)
	}
}

// encryptTotp returns the storage form of the TOTP secret, applying
// vault encryption when configured. Empty values pass through.
func (s *Service) encryptTotp(value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if s.vault == nil || !s.vault.Enabled() {
		return value, nil
	}
	if secretvault.IsEncrypted(value) {
		return value, nil
	}
	return s.vault.Encrypt(secretvault.DomainTOTP, value)
}

// decryptTotp reverses encryptTotp. Plaintext rows from before the
// vault was enabled are returned unchanged so existing users keep
// working until they next rotate their TOTP secret.
func (s *Service) decryptTotp(value string) (string, error) {
	if value == "" || !secretvault.IsEncrypted(value) {
		return value, nil
	}
	if s.vault == nil || !s.vault.Enabled() {
		return "", errors.New("auth: encrypted TOTP secret present but vault is disabled")
	}
	return s.vault.Decrypt(secretvault.DomainTOTP, value)
}

func (s *Service) verifyTotpCode(secret, code string, now time.Time) bool {
	trimmedCode := strings.TrimSpace(code)
	// During the first 90 seconds after startup, skip the past (-30s) window
	// to prevent replay of TOTP codes consumed before a restart. The consumed
	// code map is in-memory only, so after a restart an attacker could replay
	// a code that was used just before shutdown.
	elapsed := now.Sub(s.startedAt)
	skipPastWindow := elapsed >= 0 && elapsed < 90*time.Second
	for _, candidateTime := range []time.Time{now.Add(-30 * time.Second), now, now.Add(30 * time.Second)} {
		if skipPastWindow && candidateTime.Before(s.startedAt) {
			continue
		}
		expected, err := s.GenerateTotpCode(secret, candidateTime)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(trimmedCode)) == 1 {
			return true
		}
	}
	return false
}

// verifyTotpAndConsumeLocked validates a TOTP code under s.mu and records
// it in the replay-prevention map. Mirrors the consumed entry to the
// persistent store via a detached goroutine so a control-plane restart
// cannot let the same code be reused inside the verifier acceptance
// window. Caller must hold s.mu.
func (s *Service) verifyTotpAndConsumeLocked(ctx context.Context, user User, code string, now time.Time) error {
	key := totpUseKey{UserID: user.ID, Code: strings.TrimSpace(code)}
	if _, used := s.consumedTotp[key]; used {
		return ErrInvalidTotpCode
	}
	if !s.verifyTotpCode(user.TotpSecret, code, now) {
		return ErrInvalidTotpCode
	}
	s.consumedTotp[key] = now.UTC()
	bgCtx := context.WithoutCancel(ctx)
	go s.persistConsumedTotp(bgCtx, key.UserID, key.Code, now.UTC())
	return nil
}

func (s *Service) cleanupPendingTotpSetupLocked(now time.Time) {
	cutoff := now.UTC().Add(-pendingTotpSetupTTL)
	for userID, setup := range s.pendingTotpSetup {
		if setup.CreatedAt.Before(cutoff) {
			delete(s.pendingTotpSetup, userID)
		}
	}
}

// StartTotpSetup creates a short-lived TOTP setup secret for the provided user.
//
// Note: preferStartTotpSetupWithContext from request handlers.
func (s *Service) StartTotpSetup(userID string, now time.Time) (string, error) {
	return s.StartTotpSetupWithContext(context.Background(), userID, now)
}

// StartTotpSetupWithContext is the ctx-aware variant of StartTotpSetup.
func (s *Service) StartTotpSetupWithContext(ctx context.Context, userID string, now time.Time) (string, error) {
	if _, err := s.GetUserByIDWithContext(ctx, userID); err != nil {
		return "", err
	}

	secret, err := randomBase32(20)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupPendingTotpSetupLocked(now)
	s.pendingTotpSetup[userID] = pendingTotpSetup{
		Secret:    secret,
		CreatedAt: now.UTC(),
	}

	return secret, nil
}

// EnableTotp validates a pending setup and persists it as the active user TOTP secret.
//
// Note: preferEnableTotpWithContext from request handlers.
func (s *Service) EnableTotp(userID, password, totpCode string, now time.Time) (User, error) {
	return s.EnableTotpWithContext(context.Background(), userID, password, totpCode, now)
}

// EnableTotpWithContext is the ctx-aware variant of EnableTotp.
func (s *Service) EnableTotpWithContext(ctx context.Context, userID, password, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	if err := s.VerifyPassword(user.PasswordHash, password); err != nil {
		return User{}, ErrInvalidCredentials
	}

	if strings.TrimSpace(totpCode) == "" {
		return User{}, ErrTotpRequired
	}

	// Hold the lock through setup lookup and TOTP verification to prevent a
	// concurrent StartTotpSetup from replacing the pending secret between the
	// lookup and the code check (TOCTOU).
	s.mu.Lock()
	s.cleanupPendingTotpSetupLocked(now)
	setup, ok := s.pendingTotpSetup[userID]
	if !ok {
		s.mu.Unlock()
		return User{}, ErrTotpSetupNotFound
	}
	if !s.verifyTotpCode(setup.Secret, totpCode, now) {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	user.TotpEnabled = true
	user.TotpSecret = setup.Secret
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	return user, nil
}

// DisableTotp disables TOTP after validating the current password and active TOTP code.
//
// Note: preferDisableTotpWithContext from request handlers.
func (s *Service) DisableTotp(userID, password, totpCode string, now time.Time) (User, error) {
	return s.DisableTotpWithContext(context.Background(), userID, password, totpCode, now)
}

// DisableTotpWithContext is the ctx-aware variant of DisableTotp.
func (s *Service) DisableTotpWithContext(ctx context.Context, userID, password, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	if err := s.VerifyPassword(user.PasswordHash, password); err != nil {
		return User{}, ErrInvalidCredentials
	}

	trimmedCode := strings.TrimSpace(totpCode)
	if trimmedCode == "" {
		return User{}, ErrTotpRequired
	}

	// P2-SEC-08: hold the lock across the replay check, TOTP verification,
	// and the consumed-code record so two concurrent DisableTotp calls with
	// the same valid code cannot both pass. Without this, an attacker who
	// acquires one valid code can race the legitimate operator to disable
	// their second factor. Mirrors the pattern used in Authenticate.
	s.mu.Lock()
	s.cleanupExpiredSessionsLocked(now)
	key := totpUseKey{UserID: user.ID, Code: trimmedCode}
	if _, used := s.consumedTotp[key]; used {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	if !s.verifyTotpCode(user.TotpSecret, trimmedCode, now) {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	s.consumedTotp[key] = now.UTC()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	user.TotpEnabled = false
	user.TotpSecret = ""
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	return user, nil
}

// ResetTotp clears the active TOTP configuration for the provided user.
// Callers must verify that the authenticated principal is authorized to reset TOTP for the target user.
//
// Note: preferResetTotpWithContext from request handlers.
func (s *Service) ResetTotp(userID string) (User, error) {
	return s.ResetTotpWithContext(context.Background(), userID)
}

// ResetTotpWithContext is the ctx-aware variant of ResetTotp.
func (s *Service) ResetTotpWithContext(ctx context.Context, userID string) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	user.TotpEnabled = false
	user.TotpSecret = ""
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	return user, nil
}

// GenerateTotpCode derives a standard 30-second TOTP code from the stored secret.
func (s *Service) GenerateTotpCode(secret string, at time.Time) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(secret))
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return "", err
	}

	counter := uint64(at.UTC().Unix() / 30)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, decoded)
	if _, err := mac.Write(msg[:]); err != nil {
		return "", err
	}

	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0F
	value := (int(sum[offset])&0x7F)<<24 |
		int(sum[offset+1])<<16 |
		int(sum[offset+2])<<8 |
		int(sum[offset+3])
	code := value % 1_000_000

	return fmt.Sprintf("%06d", code), nil
}

func randomBase32(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
