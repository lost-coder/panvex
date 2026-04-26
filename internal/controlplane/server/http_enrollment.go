package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/security"
)

var (
	errEnrollmentTokenRevoked    = errors.New("enrollment token revoked")
	errEnrollmentStoreUnavailable = errors.New("enrollment storage is unavailable")
)

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
	// Value is the raw token. Returned ONLY at creation (handleCreate*);
	// the listing endpoint masks it to MaskedValue + Handle so an
	// operator browsing the table cannot accidentally exfiltrate a
	// usable bootstrap secret (Q4.U-S-06).
	Value          string `json:"value,omitempty"`
	MaskedValue    string `json:"masked_value,omitempty"`
	// Handle is a SHA-256 prefix of the raw value, hex-encoded. Stable
	// across reads and safe to surface in URLs (revoke endpoint accepts
	// it as an alias for the raw value).
	Handle         string `json:"handle,omitempty"`
	PanelURL       string `json:"panel_url"`
	FleetGroupID   string `json:"fleet_group_id"`
	Status         string `json:"status"`
	IssuedAtUnix   int64  `json:"issued_at_unix"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
	ConsumedAtUnix *int64 `json:"consumed_at_unix,omitempty"`
	RevokedAtUnix  *int64 `json:"revoked_at_unix,omitempty"`
}

// enrollmentTokenHandle returns the stable revoke-handle for a raw
// token value. SHA-256 prefix (16 hex chars / 64 bits) is collision-
// safe at the population sizes a control plane carries.
func enrollmentTokenHandle(rawValue string) string {
	if rawValue == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawValue))
	return hex.EncodeToString(sum[:8])
}

// enrollmentTokenMaskedValue returns the listing-safe form of the raw
// token. First 6 chars of the value followed by an ellipsis so an
// operator can disambiguate but not bootstrap.
func enrollmentTokenMaskedValue(rawValue string) string {
	if len(rawValue) <= 6 {
		return rawValue
	}
	return rawValue[:6] + "…"
}

// resolveEnrollmentTokenIdentifier maps a URL identifier (handle or
// raw value) back to the raw token value so the existing revoke
// pipeline keeps working unchanged. Walks ListEnrollmentTokens and
// matches on enrollmentTokenHandle first, then falls back to raw
// value match — that means a real value still resolves to itself.
// The walk is O(N) but enrollment tables are small and the operation
// is rare (one revoke per operator action).
func (s *Server) resolveEnrollmentTokenIdentifier(ctx context.Context, identifier string) (string, bool, error) {
	if s.store == nil {
		return identifier, true, nil
	}
	tokens, err := s.store.ListEnrollmentTokens(ctx)
	if err != nil {
		return "", false, err
	}
	for _, token := range tokens {
		if enrollmentTokenHandle(token.Value) == identifier || token.Value == identifier {
			return token.Value, true, nil
		}
	}
	return identifier, false, nil
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

		// Resolve the fleet-group reference. Empty → seed/use the
		// default group. Otherwise try id first, then fall back to
		// the unique name so operators/scripts can use the friendly
		// slug without hunting for UUIDs.
		fleetGroupID := request.FleetGroupID
		if fleetGroupID == "" {
			defaultGroup, err := s.fleetSvc.EnsureDefault(r.Context())
			if err != nil {
				s.logger.Error("ensure default fleet group failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			fleetGroupID = defaultGroup.ID
		} else {
			resolved, err := s.fleetSvc.Get(r.Context(), fleetGroupID)
			if errors.Is(err, storage.ErrNotFound) {
				resolved, err = s.fleetSvc.GetByName(r.Context(), fleetGroupID)
			}
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeError(w, http.StatusBadRequest, "fleet group not found")
					return
				}
				s.logger.Error("lookup fleet group for enrollment failed", "fleet_group_id", fleetGroupID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			fleetGroupID = resolved.ID
		}

		// R-S-14: only allow enrollment tokens for groups inside scope.
		// Done after the resolve so name → id translation runs first
		// and the operator gets the same UX even when scoped.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(fleetGroupID) {
			writeError(w, http.StatusForbidden, "fleet group outside operator scope")
			return
		}

		token, err := s.issueEnrollmentTokenWithContext(r.Context(), security.EnrollmentScope{
			FleetGroupID: fleetGroupID,
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
			"fleet_group_id": fleetGroupID,
			"ttl_seconds":    request.TTLSeconds,
		})
		settings := s.panelSettingsSnapshot()
		writeJSON(w, http.StatusCreated, createEnrollmentTokenResponse{
			Value:         token.Value,
			PanelURL:      buildAgentPublicURL(settings, s.panelRuntime, r.URL, s.trustedForwardedProto(r), r.Host),
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
			switch {
			case errors.Is(err, security.ErrEnrollmentTokenInvalid),
				errors.Is(err, security.ErrEnrollmentTokenConsumed),
				errors.Is(err, security.ErrEnrollmentTokenExpired),
				errors.Is(err, errEnrollmentTokenRevoked):
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

		tokens, err := s.listEnrollmentTokensWithContext(r.Context(), s.now(), r.URL, s.trustedForwardedProto(r), r.Host)
		if err != nil {
			s.logger.Error("list enrollment tokens failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// R-S-14: filter by fleet-group scope.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.Global {
			filtered := tokens[:0]
			for _, t := range tokens {
				if scope.IsAllowed(t.FleetGroupID) {
					filtered = append(filtered, t)
				}
			}
			tokens = filtered
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

		// Q4.U-S-06: accept the SHA-256 prefix handle as an alias for
		// the raw value. The listing endpoint no longer surfaces the
		// raw token, so the UI revoke flow uses the handle.
		if resolved, ok, err := s.resolveEnrollmentTokenIdentifier(r.Context(), value); err == nil && ok {
			value = resolved
		}

		// R-S-14: scope-check the token's fleet group before revoking.
		// We resolve the existing record first so the scope decision
		// uses the durable fleet_group_id, not the operator's input.
		existing, lookupErr := s.store.GetEnrollmentToken(r.Context(), value)
		if lookupErr != nil {
			if errors.Is(lookupErr, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "enrollment token not found")
				return
			}
			s.logger.Error("lookup enrollment token failed", "error", lookupErr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, "enrollment token not found")
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
	// S8: enrollment tokens live exclusively in storage now. Mint the
	// token value here (pure function) and persist it before returning
	// — if persistence fails we must not hand the value to the caller,
	// because a token that only exists in memory would vanish across
	// a control-plane restart or a multi-replica deploy.
	//
	// Resolve the fleet-group reference before minting so the token
	// always carries the canonical UUID, regardless of whether the
	// caller passed an id or the friendly slug. HTTP callers already
	// resolve upstream; internal callers (tests, background jobs)
	// benefit from the same convenience here.
	if scope.FleetGroupID != "" && s.fleetSvc != nil {
		resolved, err := s.fleetSvc.Get(ctx, scope.FleetGroupID)
		if errors.Is(err, storage.ErrNotFound) {
			resolved, err = s.fleetSvc.GetByName(ctx, scope.FleetGroupID)
			if errors.Is(err, storage.ErrNotFound) {
				// Not in storage yet — fleet.Service.EnsureDefault
				// covers the most common case. For non-default slugs,
				// auto-seed a row so the FK in enrollment_tokens
				// (and later agents) resolves. Matches the permissive
				// pre-redesign behaviour that internal test helpers
				// rely on.
				resolved, err = s.fleetSvc.Create(ctx, fleet.CreateInput{
					Name:  scope.FleetGroupID,
					Label: scope.FleetGroupID,
				})
			}
		}
		if err != nil {
			return security.EnrollmentToken{}, err
		}
		scope.FleetGroupID = resolved.ID
	}
	token, err := security.MintEnrollmentToken(scope, issuedAt)
	if err != nil {
		return security.EnrollmentToken{}, err
	}
	if s.store == nil {
		return security.EnrollmentToken{}, errEnrollmentStoreUnavailable
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
	// S8: consumption goes through the store only. The previous in-
	// memory fallback was a source of latent bugs across restarts and
	// replicas, and every production deploy wires a store anyway.
	if s.store == nil {
		return security.EnrollmentToken{}, errEnrollmentStoreUnavailable
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
	panelURL := buildAgentPublicURL(settings, s.panelRuntime, requestURL, forwardedProto, requestHost)
	response := make([]enrollmentTokenResponse, 0, len(records))
	for _, token := range records {
		// Q4.U-S-06: do not surface the raw value in listings — it
		// only ships at creation. List rows carry a mask + a stable
		// handle so the operator can identify and revoke without
		// being able to bootstrap.
		item := enrollmentTokenResponse{
			MaskedValue:   enrollmentTokenMaskedValue(token.Value),
			Handle:        enrollmentTokenHandle(token.Value),
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
