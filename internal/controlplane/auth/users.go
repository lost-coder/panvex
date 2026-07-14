package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// BootstrapInput describes the initial user record to create.
type BootstrapInput struct {
	Username string
	Password string
	Role     Role
}

// UpdateUserInput describes the mutable fields for one existing local user.
type UpdateUserInput struct {
	UserID      string
	Username    string
	Role        Role
	NewPassword string
	// CurrentPassword carries the caller's plaintext current password and
	// is consulted only when RequireCurrentPassword is true. Self-edits
	// must supply this to re-prove possession of the credential before
	// rotating it (S-5).
	CurrentPassword string
	// RequireCurrentPassword forces UpdateUser to verify CurrentPassword
	// against the stored hash whenever NewPassword is non-empty. Set this
	// for self-edit calls (caller and target are the same user). Admin
	// password resets on other users skip this check, so the field stays
	// false for those calls.
	RequireCurrentPassword bool
	// ExceptSessionID, when non-empty, names the one session that should
	// survive the post-update revocation pass. Set this to the caller's
	// own session ID on self-edits so the user is not logged out of the
	// browser they just used to rotate their password. Leave empty when
	// an admin rotates another user's password — every session belonging
	// to the target must be invalidated in that case.
	ExceptSessionID string
}

// CreateUser creates one local user account with TOTP disabled by default.
func (s *Service) CreateUser(ctx context.Context, input BootstrapInput, now time.Time) (User, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return User{}, ErrInvalidCredentials
	}

	if _, err := s.loadUserByUsernameCtx(ctx, username); err == nil {
		return User{}, ErrUserAlreadyExists
	} else if !errors.Is(err, ErrUserNotFound) {
		return User{}, err
	}

	user, _, err := s.BootstrapUser(ctx, BootstrapInput{
		Username: username,
		Password: input.Password,
		Role:     input.Role,
	}, now)
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return User{}, ErrUserAlreadyExists
		}
		return User{}, err
	}

	return user, nil
}

// UpdateUser mutates the mutable fields of one existing local user.
func (s *Service) UpdateUser(ctx context.Context, input UpdateUserInput, now time.Time) (User, error) {
	user, err := s.loadManagedUserByIDCtx(ctx, input.UserID)
	if err != nil {
		return User{}, err
	}

	updatedUsername := strings.TrimSpace(input.Username)
	if err := s.validateUsernameChange(ctx, user, updatedUsername); err != nil {
		return User{}, err
	}
	if err := s.validateRoleChange(ctx, user, input.Role); err != nil {
		return User{}, err
	}

	previousRole := user.Role
	previousPasswordHash := user.PasswordHash
	passwordChanged := strings.TrimSpace(input.NewPassword) != ""

	// S-5: when the caller is editing their own password they must re-prove
	// possession of the current credential. This blocks a hijacked session
	// from silently rotating the password and locking out the legitimate
	// user. Admin password resets on *other* users intentionally skip this
	// check (the admin role is the trusted authority there) and set
	// RequireCurrentPassword=false. Verify before any state mutation so a
	// failure leaves the record untouched.
	if passwordChanged && input.RequireCurrentPassword {
		if strings.TrimSpace(input.CurrentPassword) == "" {
			return User{}, ErrCurrentPasswordRequired
		}
		if err := s.VerifyPassword(previousPasswordHash, input.CurrentPassword); err != nil {
			return User{}, ErrCurrentPasswordIncorrect
		}
	}

	user.Username = updatedUsername
	user.Role = input.Role
	if err := s.applyOptionalPasswordChange(&user, input.NewPassword); err != nil {
		return User{}, err
	}

	if err := s.persistManagedUserCtx(ctx, user); err != nil {
		return User{}, err
	}

	// P2-SEC-01: revoke all active sessions whenever the password changes or
	// the role changes in either direction. Previously only role demotions
	// triggered revocation; promotions now rotate too so that any outstanding
	// session tied to the prior privilege level is forced to re-authenticate
	// under the new one. RevokeSessionsForUserExcept also clears the
	// persistent session store so a control-plane restart does not resurrect
	// the old sessions.
	//
	// S-5: ExceptSessionID lets a self-edit preserve the caller's own
	// session — the user is not logged out of the browser tab they just
	// used to rotate the credential. When the caller is an admin acting on
	// a different user, ExceptSessionID is empty and every session for the
	// target is invalidated.
	roleChanged := previousRole != input.Role
	if passwordChanged || roleChanged {
		_ = s.RevokeSessionsForUserExcept(ctx, user.ID, input.ExceptSessionID)
	}

	_ = now
	return user, nil
}

func (s *Service) validateUsernameChange(ctx context.Context, user User, updatedUsername string) error {
	if updatedUsername == "" {
		return ErrInvalidCredentials
	}
	if updatedUsername == user.Username {
		return nil
	}
	existing, err := s.loadUserByUsernameCtx(ctx, updatedUsername)
	if err == nil && existing.ID != user.ID {
		return ErrUserAlreadyExists
	}
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	return nil
}

// validateRoleChange refuses to demote the only remaining admin so the
// instance never ends up locked out of its own user-management surface.
func (s *Service) validateRoleChange(ctx context.Context, user User, newRole Role) error {
	if user.Role != RoleAdmin || newRole == RoleAdmin {
		return nil
	}
	adminCount, err := s.countAdminsCtx(ctx)
	if err != nil {
		return err
	}
	if adminCount == 1 {
		return ErrLastAdminRequired
	}
	return nil
}

func (s *Service) applyOptionalPasswordChange(user *User, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return nil
	}
	if err := validatePassword(newPassword, s.passwordMinLength()); err != nil {
		return err
	}
	hash, err := s.HashPassword(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	return nil
}

// DeleteUser removes one local user account and its active sessions.
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	user, err := s.loadManagedUserByIDCtx(ctx, userID)
	if err != nil {
		return err
	}

	if user.Role == RoleAdmin {
		adminCount, err := s.countAdminsCtx(ctx)
		if err != nil {
			return err
		}
		if adminCount == 1 {
			return ErrLastAdminRequired
		}
	}

	if err := s.userStore.DeleteUser(ctx, user.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	s.mu.Lock()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	// Drop the deleted user's active sessions from both the in-memory map and
	// the persistent session store. Done outside the lock because
	// RevokeSessionsForUser takes s.mu itself.
	_ = s.RevokeSessionsForUser(ctx, userID)

	return nil
}

func (s *Service) loadManagedUserByIDCtx(ctx context.Context, userID string) (User, error) {
	record, err := s.userStore.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return s.userFromStoredRecord(record)
}

func (s *Service) persistManagedUserCtx(ctx context.Context, user User) error {
	record := userToRecord(user)
	encrypted, err := s.encryptTotp(record.TotpSecret)
	if err != nil {
		return err
	}
	record.TotpSecret = encrypted
	if err := s.userStore.PutUser(ctx, record); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return ErrUserAlreadyExists
		}
		return err
	}
	return nil
}

func (s *Service) countAdminsCtx(ctx context.Context) (int, error) {
	records, err := s.userStore.ListUsers(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, record := range records {
		if Role(record.Role) == RoleAdmin {
			count++
		}
	}
	return count, nil
}

// BootstrapUser creates a local user with TOTP disabled by default.
func (s *Service) BootstrapUser(ctx context.Context, input BootstrapInput, now time.Time) (User, string, error) {
	if err := validatePassword(input.Password, s.passwordMinLength()); err != nil {
		return User{}, "", err
	}

	hash, err := s.HashPassword(input.Password)
	if err != nil {
		return User{}, "", err
	}

	// M-17: do not hold s.mu across the userStore.PutUser round-trip.
	// The previous form blocked every other auth-flow caller for a full
	// DB RTT. IDs are random (randomUserID), so nothing needs reserving
	// under the lock: we mint the ID, hit the store, and re-take the lock
	// only to install the row.
	id, err := randomUserID()
	if err != nil {
		return User{}, "", err
	}

	user := User{
		ID:           id,
		Username:     input.Username,
		PasswordHash: hash,
		Role:         input.Role,
		TotpEnabled:  false,
		TotpSecret:   "",
		CreatedAt:    now.UTC(),
	}

	bootstrapRecord := userToRecord(user)
	encrypted, encErr := s.encryptTotp(bootstrapRecord.TotpSecret)
	if encErr != nil {
		return User{}, "", encErr
	}
	bootstrapRecord.TotpSecret = encrypted

	if err := s.userStore.PutUser(ctx, bootstrapRecord); err != nil {
		return User{}, "", err
	}

	return user, "", nil
}

// ListUsers returns every local account with sensitive fields (password hash,
// TOTP secret) elided. Ordering (CreatedAt, then ID) comes from the store.
// (Moved out of the server package by P8.2h — was server.listUsersWithContext.)
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	records, err := s.userStore.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(records))
	for _, record := range records {
		users = append(users, User{
			ID:          record.ID,
			Username:    record.Username,
			Role:        Role(record.Role),
			TotpEnabled: record.TotpEnabled,
			CreatedAt:   record.CreatedAt.UTC(),
		})
	}
	elideSensitive(users)
	return users, nil
}

// elideSensitive zeroes the password hash and TOTP secret on every user so
// list/get responses never carry credential material.
func elideSensitive(users []User) {
	for i := range users {
		users[i].PasswordHash = ""
		users[i].TotpSecret = ""
	}
}

// LoadUsers seeds the user store with the provided accounts. Used by the
// control-plane when it is constructed with a static user list instead of a
// persistent store, and by tests.
func (s *Service) LoadUsers(ctx context.Context, users []User) error {
	for _, user := range users {
		record := userToRecord(user)
		encrypted, err := s.encryptTotp(record.TotpSecret)
		if err != nil {
			return err
		}
		record.TotpSecret = encrypted
		if err := s.userStore.PutUser(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// GetUserByID returns the user record that owns the provided identifier.
func (s *Service) GetUserByID(ctx context.Context, userID string) (User, error) {
	record, err := s.userStore.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return s.userFromStoredRecord(record)
}

func userToRecord(user User) storage.UserRecord {
	return storage.UserRecord{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         string(user.Role),
		TotpEnabled:  user.TotpEnabled,
		TotpSecret:   user.TotpSecret,
		CreatedAt:    user.CreatedAt.UTC(),
	}
}

func userFromRecord(record storage.UserRecord) User {
	return User{
		ID:           record.ID,
		Username:     record.Username,
		PasswordHash: record.PasswordHash,
		Role:         Role(record.Role),
		TotpEnabled:  record.TotpEnabled,
		TotpSecret:   record.TotpSecret,
		CreatedAt:    record.CreatedAt.UTC(),
	}
}

// loadUserByUsernameCtx returns the account owning username, or
// ErrUserNotFound when no such account exists. Callers on the LOGIN path
// (Authenticate) MUST map that to the anonymous ErrInvalidCredentials so the
// response does not distinguish "no such user" from "wrong password" — see
// the mapping in sessions.go.
func (s *Service) loadUserByUsernameCtx(ctx context.Context, username string) (User, error) {
	record, err := s.userStore.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return s.userFromStoredRecord(record)
}

func (s *Service) storeUserWithContext(ctx context.Context, user User) error {
	record := userToRecord(user)
	encrypted, err := s.encryptTotp(record.TotpSecret)
	if err != nil {
		return err
	}
	record.TotpSecret = encrypted
	return s.userStore.PutUser(ctx, record)
}

// userFromStoredRecord wraps userFromRecord with TOTP decryption. Used
// by all paths that load a user from the userStore.
func (s *Service) userFromStoredRecord(record storage.UserRecord) (User, error) {
	plaintext, err := s.decryptTotp(record.TotpSecret)
	if err != nil {
		return User{}, err
	}
	record.TotpSecret = plaintext
	return userFromRecord(record), nil
}

func randomUserID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "user-" + hex.EncodeToString(buf), nil
}
