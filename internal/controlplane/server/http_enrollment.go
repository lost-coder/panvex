package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/security"
)

var errEnrollmentTokenRevoked = errors.New("enrollment token revoked")

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

type agentBootstrapRequest struct {
	NodeName string `json:"node_name"`
	Version  string `json:"version"`
}

type agentBootstrapResponse struct {
	AgentID        string `json:"agent_id"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CAPEM          string `json:"ca_pem"`
	GRPCEndpoint   string `json:"grpc_endpoint"`
	GRPCServerName string `json:"grpc_server_name"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
}

type enrollmentTokenResponse struct {
	Value          string `json:"value"`
	EnvironmentID  string `json:"environment_id"`
	FleetGroupID   string `json:"fleet_group_id"`
	Status         string `json:"status"`
	IssuedAtUnix   int64  `json:"issued_at_unix"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
	ConsumedAtUnix *int64 `json:"consumed_at_unix,omitempty"`
	RevokedAtUnix  *int64 `json:"revoked_at_unix,omitempty"`
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

func (s *Server) handleAgentBootstrap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerTokenFromHeader(r.Header.Get("Authorization"))
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bootstrap token")
			return
		}

		var request agentBootstrapRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid bootstrap payload")
			return
		}
		if request.NodeName == "" || request.Version == "" {
			writeError(w, http.StatusBadRequest, "node_name and version are required")
			return
		}

		response, err := s.enrollAgent(agentEnrollmentRequest{
			Token:    token,
			NodeName: request.NodeName,
			Version:  request.Version,
		}, s.now())
		if err != nil {
			switch err {
			case security.ErrEnrollmentTokenInvalid, security.ErrEnrollmentTokenConsumed, security.ErrEnrollmentTokenExpired, errEnrollmentTokenRevoked:
				writeError(w, http.StatusForbidden, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}

		grpcEndpoint, grpcServerName := bootstrapGatewayAddress(r.Host)
		writeJSON(w, http.StatusOK, agentBootstrapResponse{
			AgentID:        response.AgentID,
			CertificatePEM: response.CertificatePEM,
			PrivateKeyPEM:  response.PrivateKeyPEM,
			CAPEM:          response.CAPEM,
			GRPCEndpoint:   grpcEndpoint,
			GRPCServerName: grpcServerName,
			ExpiresAtUnix:  response.ExpiresAt.Unix(),
		})
	}
}

func (s *Server) handleListEnrollmentTokens() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role == auth.RoleViewer {
			writeError(w, http.StatusForbidden, "viewer role cannot manage enrollment tokens")
			return
		}

		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "persistent store required")
			return
		}

		tokens, err := s.listEnrollmentTokens(s.now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, tokens)
	}
}

func (s *Server) handleRevokeEnrollmentToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role == auth.RoleViewer {
			writeError(w, http.StatusForbidden, "viewer role cannot manage enrollment tokens")
			return
		}

		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "persistent store required")
			return
		}

		value := chi.URLParam(r, "value")
		if value == "" {
			writeError(w, http.StatusBadRequest, "token value is required")
			return
		}

		revoked, changed, err := s.revokeEnrollmentToken(value, s.now())
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "enrollment token not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if changed {
			s.appendAudit(session.UserID, "agents.enrollment.revoke", revoked.Value, map[string]any{
				"environment_id": revoked.EnvironmentID,
				"fleet_group_id": revoked.FleetGroupID,
			})
		}

		w.WriteHeader(http.StatusNoContent)
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
	if token.RevokedAt != nil {
		return security.EnrollmentToken{}, errEnrollmentTokenRevoked
	}

	if now.UTC().After(token.ExpiresAt.UTC()) {
		return security.EnrollmentToken{}, security.ErrEnrollmentTokenExpired
	}

	consumed, err := s.store.ConsumeEnrollmentToken(context.Background(), value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			latest, latestErr := s.store.GetEnrollmentToken(context.Background(), value)
			if latestErr != nil {
				if errors.Is(latestErr, storage.ErrNotFound) {
					return security.EnrollmentToken{}, security.ErrEnrollmentTokenInvalid
				}
				return security.EnrollmentToken{}, latestErr
			}
			if latest.RevokedAt != nil {
				return security.EnrollmentToken{}, errEnrollmentTokenRevoked
			}
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

func (s *Server) listEnrollmentTokens(now time.Time) ([]enrollmentTokenResponse, error) {
	records, err := s.store.ListEnrollmentTokens(context.Background())
	if err != nil {
		return nil, err
	}

	response := make([]enrollmentTokenResponse, 0, len(records))
	for _, token := range records {
		item := enrollmentTokenResponse{
			Value:         token.Value,
			EnvironmentID: token.EnvironmentID,
			FleetGroupID:  token.FleetGroupID,
			Status:        enrollmentTokenStatus(token, now),
			IssuedAtUnix:  token.IssuedAt.UTC().Unix(),
			ExpiresAtUnix: token.ExpiresAt.UTC().Unix(),
		}
		if token.ConsumedAt != nil {
			value := token.ConsumedAt.UTC().Unix()
			item.ConsumedAtUnix = &value
		}
		if token.RevokedAt != nil {
			value := token.RevokedAt.UTC().Unix()
			item.RevokedAtUnix = &value
		}
		response = append(response, item)
	}

	return response, nil
}

func (s *Server) revokeEnrollmentToken(value string, now time.Time) (storage.EnrollmentTokenRecord, bool, error) {
	token, err := s.store.RevokeEnrollmentToken(context.Background(), value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			latest, latestErr := s.store.GetEnrollmentToken(context.Background(), value)
			if latestErr != nil {
				return storage.EnrollmentTokenRecord{}, false, latestErr
			}
			return latest, false, nil
		}
		return storage.EnrollmentTokenRecord{}, false, err
	}

	if token.RevokedAt == nil || !token.RevokedAt.Equal(now.UTC()) {
		return token, false, nil
	}

	return token, true, nil
}

func enrollmentTokenStatus(token storage.EnrollmentTokenRecord, now time.Time) string {
	if token.RevokedAt != nil {
		return "revoked"
	}
	if token.ConsumedAt != nil {
		return "consumed"
	}
	if now.UTC().After(token.ExpiresAt.UTC()) {
		return "expired"
	}

	return "active"
}

func bearerTokenFromHeader(value string) (string, bool) {
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}

	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == "" {
		return "", false
	}

	return token, true
}

func bootstrapGatewayAddress(host string) (string, string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "127.0.0.1:8443", "control-plane.panvex.internal"
	}

	serverName := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		serverName = parsedHost
	}

	if strings.Contains(serverName, ":") && !strings.Contains(serverName, "]") {
		return "[" + serverName + "]:8443", serverName
	}

	return net.JoinHostPort(serverName, "8443"), serverName
}
