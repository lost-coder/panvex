package server

import (
	"net/http"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
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

		token, err := s.enrollment.IssueToken(security.EnrollmentScope{
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
