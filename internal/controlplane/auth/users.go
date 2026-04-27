package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// CreateUser creates one local user account with TOTP disabled by default.
//
// Note: preferCreateUserWithContext from request handlers.
func (s *Service) CreateUser(input BootstrapInput, now time.Time) (User, error) {
	return s.CreateUserWithContext(context.Background(), input, now)
}

// CreateUserWithContext is the ctx-aware variant of CreateUser.
func (s *Service) CreateUserWithContext(ctx context.Context, input BootstrapInput, now time.Time) (User, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return User{}, ErrInvalidCredentials
	}

	if _, err := s.loadUserByUsernameCtx(ctx, username); err == nil {
		return User{}, ErrUserAlreadyExists
	} else if !errors.Is(err, ErrInvalidCredentials) {
		return User{}, err
	}

	user, _, err := s.BootstrapUserWithContext(ctx, BootstrapInput{
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
//
// Note: preferUpdateUserWithContext from request handlers.
func (s *Service) UpdateUser(input UpdateUserInput, now time.Time) (User, error) {
	return s.UpdateUserWithContext(context.Background(), input, now)
}

// UpdateUserWithContext is the ctx-aware variant of UpdateUser.
func (s *Service) UpdateUserWithContext(ctx context.Context, input UpdateUserInput, now time.Time) (User, error) {
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
	// under the new one. RevokeSessionsForUser also clears the persistent
	// session store so a control-plane restart does not resurrect the old
	// sessions.
	passwordChanged := strings.TrimSpace(input.NewPassword) != ""
	roleChanged := previousRole != input.Role
	if passwordChanged || roleChanged {
		_ = s.RevokeSessionsForUserWithContext(ctx, user.ID)
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
	if !(user.Role == RoleAdmin && newRole != RoleAdmin) {
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
	if err := validatePassword(newPassword); err != nil {
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
//
// Note: preferDeleteUserWithContext from request handlers.
func (s *Service) DeleteUser(userID string) error {
	return s.DeleteUserWithContext(context.Background(), userID)
}

// DeleteUserWithContext is the ctx-aware variant of DeleteUser.
func (s *Service) DeleteUserWithContext(ctx context.Context, userID string) error {
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
	delete(s.users, user.Username)
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	// Drop the deleted user's active sessions from both the in-memory map and
	// the persistent session store. Done outside the lock because
	// RevokeSessionsForUser takes s.mu itself.
	_ = s.RevokeSessionsForUserWithContext(ctx, userID)

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

	for _, user := range s.users {
		if user.ID == userID {
			return user, nil
		}
	}

	return User{}, ErrUserNotFound
}

func (s *Service) persistManagedUserCtx(ctx context.Context, user User) error {
	previousUsername := ""

	s.mu.Lock()
	for username, existingUser := range s.users {
		if existingUser.ID == user.ID {
			previousUsername = username
			break
		}
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
	if previousUsername != "" && previousUsername != user.Username {
		delete(s.users, previousUsername)
	}
	s.users[user.Username] = user
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
