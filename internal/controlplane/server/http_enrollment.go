package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/security"
)

var errEnrollmentTokenRevoked = errors.New("enrollment token revoked")

const (
	agentCertificateRecoveryProofSkew  = 5 * time.Minute
	// agentCertificateRecoveryGraceWindow defines how long after certificate
	// expiry an agent can still use the recovery flow. Shorter values reduce
	// the attack surface of a compromised agent credential.
	agentCertificateRecoveryGraceWindow = 24 * time.Hour
	defaultAgentCertificateRecoveryGrantTTL = 15 * time.Minute
	maxAgentCertificateRecoveryGrantTTL = time.Hour
)

type createEnrollmentTokenRequest struct {
	FleetGroupID string `json:"fleet_group_id"`
	TTLSeconds   int    `json:"ttl_seconds"`
}

type createEnrollmentTokenResponse struct {
	Value         string `json:"value"`
	PanelURL      string `json:"panel_url"`
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

type agentCertificateRecoveryRequest struct {
	AgentID            string `json:"agent_id"`
	CertificatePEM     string `json:"certificate_pem"`
	ProofTimestampUnix int64  `json:"proof_timestamp_unix"`
	ProofNonce         string `json:"proof_nonce"`
	ProofSignature     string `json:"proof_signature"`
}

type agentCertificateRecoveryGrantRequest struct {
	TTLSeconds int `json:"ttl_seconds"`
}

type agentCertificateRecoveryGrantResponse struct {
	AgentID       string `json:"agent_id"`
	Status        string `json:"status"`
	IssuedAtUnix  int64  `json:"issued_at_unix"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	UsedAtUnix    *int64 `json:"used_at_unix,omitempty"`
	RevokedAtUnix *int64 `json:"revoked_at_unix,omitempty"`
}

type enrollmentTokenResponse struct {
	Value          string `json:"value"`
	PanelURL       string `json:"panel_url"`
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

		token, err := s.issueEnrollmentTokenWithContext(r.Context(), security.EnrollmentScope{
			FleetGroupID: request.FleetGroupID,
			TTL:          time.Duration(request.TTLSeconds) * time.Second,
		}, s.now())
		if err != nil {
			if errors.Is(err, security.ErrEnrollmentTokenTTLRequired) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.logger.Error("create enrollment token failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.enrollment.create", maskToken(token.Value), map[string]any{
			"fleet_group_id": request.FleetGroupID,
			"ttl_seconds":    request.TTLSeconds,
		})
		settings := s.panelSettingsSnapshot()
		writeJSON(w, http.StatusCreated, createEnrollmentTokenResponse{
			Value:         token.Value,
			PanelURL:      buildPanelPublicURL(settings, s.panelRuntime, r.URL, r.Header.Get("X-Forwarded-Proto"), r.Host),
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

		response, err := s.enrollAgentWithContext(r.Context(), agentEnrollmentRequest{
			Token:    token,
			NodeName: request.NodeName,
			Version:  request.Version,
		}, s.now())
		if err != nil {
			switch err {
			case security.ErrEnrollmentTokenInvalid, security.ErrEnrollmentTokenConsumed, security.ErrEnrollmentTokenExpired, errEnrollmentTokenRevoked:
				writeError(w, http.StatusForbidden, err.Error())
			default:
				s.logger.Error("agent bootstrap failed", "node_name", request.NodeName, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		grpcEndpoint, grpcServerName := s.bootstrapGatewayAddress(r.Host)
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

		tokens, err := s.listEnrollmentTokensWithContext(r.Context(), s.now(), r.URL, r.Header.Get("X-Forwarded-Proto"), r.Host)
		if err != nil {
			s.logger.Error("list enrollment tokens failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
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

		revoked, changed, err := s.revokeEnrollmentTokenWithContext(r.Context(), value, s.now())
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "enrollment token not found")
				return
			}
			s.logger.Error("revoke enrollment token failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if changed {
			s.appendAuditWithContext(r.Context(), session.UserID, "agents.enrollment.revoke", maskToken(revoked.Value), map[string]any{
				"fleet_group_id": revoked.FleetGroupID,
			})
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) issueEnrollmentToken(scope security.EnrollmentScope, issuedAt time.Time) (security.EnrollmentToken, error) {
	return s.issueEnrollmentTokenWithContext(context.Background(), scope, issuedAt)
}

func (s *Server) issueEnrollmentTokenWithContext(ctx context.Context, scope security.EnrollmentScope, issuedAt time.Time) (security.EnrollmentToken, error) {
	token, err := s.enrollment.IssueToken(scope, issuedAt)
	if err != nil {
		return security.EnrollmentToken{}, err
	}

	if s.store == nil {
		return token, nil
	}

	if err := s.store.PutEnrollmentToken(ctx, storage.EnrollmentTokenRecord{
		Value:        token.Value,
		FleetGroupID: token.FleetGroupID,
		IssuedAt:     token.IssuedAt.UTC(),
		ExpiresAt:    token.ExpiresAt.UTC(),
	}); err != nil {
		return security.EnrollmentToken{}, err
	}

	return token, nil
}

func (s *Server) consumeEnrollmentToken(value string, now time.Time) (security.EnrollmentToken, error) {
	return s.consumeEnrollmentTokenWithContext(context.Background(), value, now)
}

func (s *Server) consumeEnrollmentTokenWithContext(ctx context.Context, value string, now time.Time) (security.EnrollmentToken, error) {
	if s.store == nil {
		return s.enrollment.ConsumeToken(value, now)
	}

	token, err := s.store.GetEnrollmentToken(ctx, value)
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

	consumed, err := s.store.ConsumeEnrollmentToken(ctx, value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			latest, latestErr := s.store.GetEnrollmentToken(ctx, value)
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
		Value:        consumed.Value,
		FleetGroupID: consumed.FleetGroupID,
		IssuedAt:     consumed.IssuedAt.UTC(),
		ExpiresAt:    consumed.ExpiresAt.UTC(),
	}, nil
}

func (s *Server) listEnrollmentTokens(now time.Time, requestURL *url.URL, forwardedProto string, requestHost string) ([]enrollmentTokenResponse, error) {
	return s.listEnrollmentTokensWithContext(context.Background(), now, requestURL, forwardedProto, requestHost)
}

func (s *Server) listEnrollmentTokensWithContext(ctx context.Context, now time.Time, requestURL *url.URL, forwardedProto string, requestHost string) ([]enrollmentTokenResponse, error) {
	records, err := s.store.ListEnrollmentTokens(ctx)
	if err != nil {
		return nil, err
	}

	settings := s.panelSettingsSnapshot()
	panelURL := buildPanelPublicURL(settings, s.panelRuntime, requestURL, forwardedProto, requestHost)
	response := make([]enrollmentTokenResponse, 0, len(records))
	for _, token := range records {
		item := enrollmentTokenResponse{
			Value:         token.Value,
			PanelURL:      panelURL,
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
	return s.revokeEnrollmentTokenWithContext(context.Background(), value, now)
}

func (s *Server) revokeEnrollmentTokenWithContext(ctx context.Context, value string, now time.Time) (storage.EnrollmentTokenRecord, bool, error) {
	token, err := s.store.RevokeEnrollmentToken(ctx, value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			latest, latestErr := s.store.GetEnrollmentToken(ctx, value)
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

func (s *Server) bootstrapGatewayAddress(host string) (string, string) {
	settings := s.panelSettingsSnapshot()
	if settings.GRPCPublicEndpoint != "" {
		return settings.GRPCPublicEndpoint, "control-plane.panvex.internal"
	}

	return defaultBootstrapGatewayAddress(host)
}

func defaultBootstrapGatewayAddress(host string) (string, string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "127.0.0.1:8443", "control-plane.panvex.internal"
	}

	serverName := "control-plane.panvex.internal"
	endpointHost := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		endpointHost = parsedHost
	}

	// The gateway certificate still uses the internal control-plane identity.
	// Keep that TLS server name stable while deriving the network endpoint from
	// the bootstrap request host.
	if strings.Contains(endpointHost, ":") && !strings.Contains(endpointHost, "]") {
		return "[" + endpointHost + "]:8443", serverName
	}

	return net.JoinHostPort(endpointHost, "8443"), serverName
}
