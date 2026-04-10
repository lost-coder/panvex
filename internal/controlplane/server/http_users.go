package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
)

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TotpEnabled bool   `json:"totp_enabled"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type createUserRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

type updateUserRequest struct {
	Username    string `json:"username"`
	Role        string `json:"role"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		users, err := s.listUsersWithContext(r.Context())
		if err != nil {
			s.logger.Error("list users failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		response := make([]userResponse, 0, len(users))
		for _, listedUser := range users {
			resp := userResponse{
				ID:          listedUser.ID,
				Username:    listedUser.Username,
				Role:        string(listedUser.Role),
				TotpEnabled: listedUser.TotpEnabled,
			}
			if !listedUser.CreatedAt.IsZero() {
				resp.CreatedAt = listedUser.CreatedAt.Format(time.RFC3339)
			}
			response = append(response, resp)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleCreateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		var request createUserRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid user payload")
			return
		}

		createdUser, err := s.auth.CreateUser(auth.BootstrapInput{
			Username: request.Username,
			Password: request.Password,
			Role:     auth.Role(request.Role),
		}, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrUserAlreadyExists):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, auth.ErrPasswordTooWeak):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				s.logger.Error("create user failed", "username", request.Username, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "users.create", createdUser.ID, map[string]any{
			"username": createdUser.Username,
			"role":     createdUser.Role,
		})
		writeJSON(w, http.StatusCreated, userResponse{
			ID:          createdUser.ID,
			Username:    createdUser.Username,
			Role:        string(createdUser.Role),
			TotpEnabled: createdUser.TotpEnabled,
		})
	}
}

func (s *Server) handleUpdateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		targetUserID := chi.URLParam(r, "id")
		if targetUserID == "" {
			writeError(w, http.StatusBadRequest, "user id is required")
			return
		}

		var request updateUserRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid user payload")
			return
		}

		updatedUser, err := s.auth.UpdateUser(auth.UpdateUserInput{
			UserID:      targetUserID,
			Username:    request.Username,
			Role:        auth.Role(request.Role),
			NewPassword: request.NewPassword,
		}, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrUserNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, auth.ErrUserAlreadyExists):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, auth.ErrLastAdminRequired):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, auth.ErrPasswordTooWeak):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				s.logger.Error("update user failed", "user_id", targetUserID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "users.update", updatedUser.ID, map[string]any{
			"username": updatedUser.Username,
			"role":     updatedUser.Role,
		})
		writeJSON(w, http.StatusOK, userResponse{
			ID:          updatedUser.ID,
			Username:    updatedUser.Username,
			Role:        string(updatedUser.Role),
			TotpEnabled: updatedUser.TotpEnabled,
		})
	}
}

func (s *Server) handleDeleteUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		targetUserID := chi.URLParam(r, "id")
		if targetUserID == "" {
			writeError(w, http.StatusBadRequest, "user id is required")
			return
		}

		if targetUserID == session.UserID {
			writeError(w, http.StatusBadRequest, "admin cannot delete own account")
			return
		}

		if err := s.auth.DeleteUser(targetUserID); err != nil {
			switch {
			case errors.Is(err, auth.ErrUserNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, auth.ErrLastAdminRequired):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				s.logger.Error("delete user failed", "user_id", targetUserID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "users.delete", targetUserID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleResetUserTotp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		targetUserID := chi.URLParam(r, "id")
		if targetUserID == "" {
			writeError(w, http.StatusBadRequest, "user id is required")
			return
		}

		if targetUserID == session.UserID {
			writeError(w, http.StatusBadRequest, "admin cannot reset own totp from users api")
			return
		}

		if _, err := s.auth.ResetTotp(targetUserID); err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusNotFound, "user not found")
				return
			}
			s.logger.Error("reset user totp failed", "user_id", targetUserID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "auth.totp.reset_by_admin", targetUserID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) listUsers() ([]auth.User, error) {
	return s.listUsersWithContext(context.Background())
}

func (s *Server) listUsersWithContext(ctx context.Context) ([]auth.User, error) {
	if s.store == nil {
		users := s.auth.SnapshotUsers()
		sort.Slice(users, func(left int, right int) bool {
			if users[left].CreatedAt.Equal(users[right].CreatedAt) {
				return users[left].ID < users[right].ID
			}
			return users[left].CreatedAt.Before(users[right].CreatedAt)
		})
		return users, nil
	}

	records, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]auth.User, 0, len(records))
	for _, record := range records {
		users = append(users, auth.User{
			ID:           record.ID,
			Username:     record.Username,
			PasswordHash: record.PasswordHash,
			Role:         auth.Role(record.Role),
			TotpEnabled:  record.TotpEnabled,
			TotpSecret:   record.TotpSecret,
			CreatedAt:    record.CreatedAt.UTC(),
		})
	}

	return users, nil
}
