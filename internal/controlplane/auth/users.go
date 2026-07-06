package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
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
	} else if !errors.Is(err, ErrInvalidCredentials) {
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
	if err != nil && !errors.Is(err, ErrInvalidCredentials) {
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

	if s.userStore != nil {
		if err := s.userStore.DeleteUser(ctx, userID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrUserNotFound
			}
			return err
		}
	}

	s.mu.Lock()
	s.deleteUserLocked(user)
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	// Drop the deleted user's active sessions from both the in-memory map and
	// the persistent session store. Done outside the lock because
	// RevokeSessionsForUser takes s.mu itself.
	_ = s.RevokeSessionsForUser(ctx, userID)

	return nil
}

func (s *Service) loadManagedUserByIDCtx(ctx context.Context, userID string) (User, error) {
	if s.userStore != nil {
		record, err := s.userStore.GetUserByID(ctx, userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrUserNotFound
			}
			return User{}, err
		}
		return s.userFromStoredRecord(record)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if user, ok := s.usersByID[userID]; ok {
		return user, nil
	}

	return User{}, ErrUserNotFound
}

func (s *Service) persistManagedUserCtx(ctx context.Context, user User) error {
	previousUsername := ""

	s.mu.Lock()
	if existing, ok := s.usersByID[user.ID]; ok {
		previousUsername = existing.Username
	}
	s.mu.Unlock()

	if s.userStore != nil {
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
	}

	s.mu.Lock()
	s.putUserLocked(user, previousUsername)
	s.mu.Unlock()

	return nil
}

func (s *Service) countAdminsCtx(ctx context.Context) (int, error) {
	if s.userStore != nil {
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

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, user := range s.users {
		if user.Role == RoleAdmin {
			count++
		}
	}
	return count, nil
}

// putUserLocked installs (or updates) a user record in both indices.
// Caller must hold s.mu for write. previousUsername (when non-empty)
// removes the stale name->record entry left over from a username
// rename so the username index does not double-up.
func (s *Service) putUserLocked(user User, previousUsername string) {
	if previousUsername != "" && previousUsername != user.Username {
		delete(s.users, previousUsername)
	}
	s.users[user.Username] = user
	if user.ID != "" {
		s.usersByID[user.ID] = user
	}
}

// deleteUserLocked removes a user from both indices. Caller must hold
// s.mu for write.
func (s *Service) deleteUserLocked(user User) {
	delete(s.users, user.Username)
	if user.ID != "" {
		delete(s.usersByID, user.ID)
	}
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

	if s.userStore == nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		id, err := randomUserID()
		if err != nil {
			return User{}, "", err
		}
		s.sequence++
		user := User{
			ID:           id,
			Username:     input.Username,
			PasswordHash: hash,
			Role:         input.Role,
			TotpEnabled:  false,
			TotpSecret:   "",
			CreatedAt:    now.UTC(),
		}
		s.putUserLocked(user, "")

		return user, "", nil
	}

	users, err := s.userStore.ListUsers(ctx)
	if err != nil {
		return User{}, "", err
	}

	// M-17: do not hold s.mu across the userStore.PutUser round-trip.
	// The previous form blocked every other auth-flow caller for a full
	// DB RTT. Now we (a) reserve the next sequence/ID under the lock,
	// (b) drop the lock, (c) hit the store, (d) re-take the lock only
	// to install the row. Failure paths re-take the lock briefly to
	// roll the sequence back so concurrent bootstraps never re-use an
	// allocated ID.
	id, err := randomUserID()
	if err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.sequence = maxSequence(s.sequence, maxUserSequence(users))
	s.sequence++
	s.mu.Unlock()

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
		s.mu.Lock()
		s.sequence--
		s.mu.Unlock()
		return User{}, "", encErr
	}
	bootstrapRecord.TotpSecret = encrypted

	if err := s.userStore.PutUser(ctx, bootstrapRecord); err != nil {
		s.mu.Lock()
		s.sequence--
		s.mu.Unlock()
		return User{}, "", err
	}

	s.mu.Lock()
	s.putUserLocked(user, "")
	s.mu.Unlock()

	return user, "", nil
}

// ListUsers returns every local account with sensitive fields (password hash,
// TOTP secret) elided. It reads from the persistent user store when one is
// wired (NewServiceWithStore), sorting by CreatedAt then ID for a stable
// admin view; without a store it falls back to the in-memory snapshot.
// (Moved out of the server package by P8.2h — was server.listUsersWithContext.)
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	if s.userStore == nil {
		users := s.SnapshotUsers()
		sortUsers(users)
		elideSensitive(users)
		return users, nil
	}

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

// sortUsers orders users by CreatedAt then ID (stable admin view).
func sortUsers(users []User) {
	sort.Slice(users, func(left, right int) bool {
		if users[left].CreatedAt.Equal(users[right].CreatedAt) {
			return users[left].ID < users[right].ID
		}
		return users[left].CreatedAt.Before(users[right].CreatedAt)
	})
}

// elideSensitive zeroes the password hash and TOTP secret on every user so
// list/get responses never carry credential material.
func elideSensitive(users []User) {
	for i := range users {
		users[i].PasswordHash = ""
		users[i].TotpSecret = ""
	}
}

// SnapshotUsers returns a copy of the current local-account state.
func (s *Service) SnapshotUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]User, 0, len(s.users))
	for _, user := range s.users {
		result = append(result, user)
	}

	return result
}

// LoadUsers replaces the current local-account state with the provided users.
func (s *Service) LoadUsers(users []User) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]User, len(users))
	s.usersByID = make(map[string]User, len(users))
	for _, user := range users {
		s.putUserLocked(user, "")
	}
}

// GetUserByID returns the user record that owns the provided identifier.
func (s *Service) GetUserByID(ctx context.Context, userID string) (User, error) {
	if s.userStore != nil {
		record, err := s.userStore.GetUserByID(ctx, userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return s.userFromStoredRecord(record)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if user, ok := s.usersByID[userID]; ok {
		return user, nil
	}

	return User{}, ErrInvalidCredentials
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

func (s *Service) loadUserByUsernameCtx(ctx context.Context, username string) (User, error) {
	s.mu.RLock()
	userStore := s.userStore
	s.mu.RUnlock()

	if userStore != nil {
		record, err := userStore.GetUserByUsername(ctx, username)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return s.userFromStoredRecord(record)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok {
		return User{}, ErrInvalidCredentials
	}

	return user, nil
}

func (s *Service) storeUserWithContext(ctx context.Context, user User) error {
	if s.userStore != nil {
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

	s.mu.Lock()
	s.putUserLocked(user, "")
	s.mu.Unlock()

	return nil
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

func maxUserSequence(users []storage.UserRecord) uint64 {
	var maxValue uint64
	for _, user := range users {
		if !strings.HasPrefix(user.ID, "user-") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(user.ID, "user-"), 10, 64)
		if err != nil {
			continue
		}
		if value > maxValue {
			maxValue = value
		}
	}

	return maxValue
}

func maxSequence(left, right uint64) uint64 {
	if right > left {
		return right
	}

	return left
}

func randomUserID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "user-" + hex.EncodeToString(buf), nil
}
