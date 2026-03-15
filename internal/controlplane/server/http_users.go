package server

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
)

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TotpEnabled bool   `json:"totp_enabled"`
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

		users, err := s.listUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		response := make([]userResponse, 0, len(users))
		for _, listedUser := range users {
			response = append(response, userResponse{
				ID:          listedUser.ID,
				Username:    listedUser.Username,
				Role:        string(listedUser.Role),
				TotpEnabled: listedUser.TotpEnabled,
			})
		}

		writeJSON(w, http.StatusOK, response)
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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.appendAudit(session.UserID, "auth.totp.reset_by_admin", targetUserID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) listUsers() ([]auth.User, error) {
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

	records, err := s.store.ListUsers(context.Background())
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
