package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

// CreateUser creates one local user account with TOTP disabled by default.
func (s *Service) CreateUser(input BootstrapInput, now time.Time) (User, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return User{}, ErrInvalidCredentials
	}

	if _, err := s.loadUserByUsername(username); err == nil {
		return User{}, ErrUserAlreadyExists
	} else if !errors.Is(err, ErrInvalidCredentials) {
		return User{}, err
	}

	user, _, err := s.BootstrapUser(BootstrapInput{
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
func (s *Service) UpdateUser(input UpdateUserInput, now time.Time) (User, error) {
	user, err := s.loadManagedUserByID(input.UserID)
	if err != nil {
		return User{}, err
	}

	updatedUsername := strings.TrimSpace(input.Username)
	if updatedUsername == "" {
		return User{}, ErrInvalidCredentials
	}
	if updatedUsername != user.Username {
		existing, err := s.loadUserByUsername(updatedUsername)
		if err == nil && existing.ID != user.ID {
			return User{}, ErrUserAlreadyExists
		}
		if err != nil && !errors.Is(err, ErrInvalidCredentials) {
			return User{}, err
		}
	}

	if user.Role == RoleAdmin && input.Role != RoleAdmin {
		adminCount, err := s.countAdmins()
		if err != nil {
			return User{}, err
		}
		if adminCount == 1 {
			return User{}, ErrLastAdminRequired
		}
	}

	previousRole := user.Role
	user.Username = updatedUsername
	user.Role = input.Role
	if strings.TrimSpace(input.NewPassword) != "" {
		if err := validatePasswordComplexity(input.NewPassword); err != nil {
			return User{}, err
		}
		hash, err := s.HashPassword(input.NewPassword)
		if err != nil {
			return User{}, err
		}
		user.PasswordHash = hash
	}

	passwordChanged := strings.TrimSpace(input.NewPassword) != ""
	roleDemoted := previousRole != input.Role && isRoleDemotion(previousRole, input.Role)

	if err := s.persistManagedUser(user); err != nil {
		return User{}, err
	}

	// Revoke all active sessions when the password changes or the role is
	// demoted so that stolen sessions cannot outlive credential rotation.
	if passwordChanged || roleDemoted {
		s.mu.Lock()
		for sessionID, session := range s.sessions {
			if session.UserID == user.ID {
				delete(s.sessions, sessionID)
			}
		}
		s.mu.Unlock()
	}

	_ = now
	return user, nil
}

// DeleteUser removes one local user account and its active sessions.
func (s *Service) DeleteUser(userID string) error {
	user, err := s.loadManagedUserByID(userID)
	if err != nil {
		return err
	}

	if user.Role == RoleAdmin {
		adminCount, err := s.countAdmins()
		if err != nil {
			return err
		}
		if adminCount == 1 {
			return ErrLastAdminRequired
		}
	}

	if s.userStore != nil {
		if err := s.userStore.DeleteUser(context.Background(), userID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrUserNotFound
			}
			return err
		}
	}

	s.mu.Lock()
	delete(s.users, user.Username)
	delete(s.pendingTotpSetup, userID)
	for sessionID, session := range s.sessions {
		if session.UserID == userID {
			delete(s.sessions, sessionID)
		}
	}
	s.mu.Unlock()

	return nil
}

// isRoleDemotion returns true when the new role has fewer privileges than the
// previous role. Used to decide whether active sessions should be revoked.
func isRoleDemotion(previous Role, next Role) bool {
	return roleWeight(next) < roleWeight(previous)
}

func roleWeight(r Role) int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

func (s *Service) loadManagedUserByID(userID string) (User, error) {
	if s.userStore != nil {
		record, err := s.userStore.GetUserByID(context.Background(), userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrUserNotFound
			}
			return User{}, err
		}
		return userFromRecord(record), nil
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

func (s *Service) persistManagedUser(user User) error {
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
		if err := s.userStore.PutUser(context.Background(), userToRecord(user)); err != nil {
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

func (s *Service) countAdmins() (int, error) {
	if s.userStore != nil {
		records, err := s.userStore.ListUsers(context.Background())
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
