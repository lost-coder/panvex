package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/security"
)

type createEnrollmentTokenRequest struct {
	EnvironmentID string `json:"environment_id"`
	FleetGroupID  string `json:"fleet_group_id"`
	TTLSeconds    int    `json:"ttl_seconds"`
}

type createEnrollmentTokenResponse struct {
	Value         string `json:"value"`
	EnvironmentID string `json:"environment_id"`
	FleetGroupID  string `json:"fleet_group_id"`
	IssuedAtUnix  int64  `json:"issued_at_unix"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	CAPEM         string `json:"ca_pem"`
}

func (s *Server) handleCreateEnrollmentToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role == auth.RoleViewer {
			writeError(w, http.StatusForbidden, "viewer role cannot create enrollment tokens")
			return
		}

		var request createEnrollmentTokenRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid enrollment payload")
			return
		}

		token, err := s.issueEnrollmentToken(security.EnrollmentScope{
			EnvironmentID: request.EnvironmentID,
			FleetGroupID:  request.FleetGroupID,
			TTL:           time.Duration(request.TTLSeconds) * time.Second,
		}, s.now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.appendAudit(session.UserID, "agents.enrollment.create", token.Value, map[string]any{
			"environment_id": request.EnvironmentID,
			"fleet_group_id": request.FleetGroupID,
			"ttl_seconds":    request.TTLSeconds,
		})
		writeJSON(w, http.StatusCreated, createEnrollmentTokenResponse{
			Value:         token.Value,
			EnvironmentID: token.EnvironmentID,
			FleetGroupID:  token.FleetGroupID,
			IssuedAtUnix:  token.IssuedAt.Unix(),
			ExpiresAtUnix: token.ExpiresAt.Unix(),
			CAPEM:         s.authority.caPEM,
		})
	}
}

func (s *Server) issueEnrollmentToken(scope security.EnrollmentScope, issuedAt time.Time) (security.EnrollmentToken, error) {
	token, err := s.enrollment.IssueToken(scope, issuedAt)
	if err != nil {
		return security.EnrollmentToken{}, err
	}

	if s.store == nil {
		return token, nil
	}

	if err := s.store.PutEnrollmentToken(context.Background(), storage.EnrollmentTokenRecord{
		Value:         token.Value,
		EnvironmentID: token.EnvironmentID,
		FleetGroupID:  token.FleetGroupID,
		IssuedAt:      token.IssuedAt.UTC(),
		ExpiresAt:     token.ExpiresAt.UTC(),
	}); err != nil {
		return security.EnrollmentToken{}, err
	}

	return token, nil
}

func (s *Server) consumeEnrollmentToken(value string, now time.Time) (security.EnrollmentToken, error) {
	if s.store == nil {
		return s.enrollment.ConsumeToken(value, now)
	}

	token, err := s.store.GetEnrollmentToken(context.Background(), value)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return security.EnrollmentToken{}, security.ErrEnrollmentTokenInvalid
		}
		return security.EnrollmentToken{}, err
	}

	if token.ConsumedAt != nil {
		return security.EnrollmentToken{}, security.ErrEnrollmentTokenConsumed
	}

	if now.UTC().After(token.ExpiresAt.UTC()) {
		return security.EnrollmentToken{}, security.ErrEnrollmentTokenExpired
	}

	consumed, err := s.store.ConsumeEnrollmentToken(context.Background(), value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return security.EnrollmentToken{}, security.ErrEnrollmentTokenConsumed
		}
		if errors.Is(err, storage.ErrNotFound) {
			return security.EnrollmentToken{}, security.ErrEnrollmentTokenInvalid
		}
		return security.EnrollmentToken{}, err
	}

	return security.EnrollmentToken{
		Value:         consumed.Value,
		EnvironmentID: consumed.EnvironmentID,
		FleetGroupID:  consumed.FleetGroupID,
		IssuedAt:      consumed.IssuedAt.UTC(),
		ExpiresAt:     consumed.ExpiresAt.UTC(),
	}, nil
}
