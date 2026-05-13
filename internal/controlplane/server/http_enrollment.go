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
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/security"
)

var (
	errEnrollmentTokenRevoked    = errors.New("enrollment token revoked")
	errEnrollmentStoreUnavailable = errors.New("enrollment storage is unavailable")
)

// defaultGatewayHost is the TLS server name carried by control-plane gateway
// certificates. Hoisted out of the bootstrap-address helpers so the literal
// is not duplicated three times (Sonar S1192).
const defaultGatewayHost = "control-plane.panvex.internal"

const (
	agentCertificateRecoveryProofSkew  = 5 * time.Minute
	// agentCertificateRecoveryGraceWindow defines how long after certificate
	// expiry an agent can still use the recovery flow. Shorter values reduce
	// the attack surface of a compromised agent credential. L-9: tightened
	// from 24h to 2h — long enough that a transient outage during cert
	// rotation does not need a manual panel-side grant, short enough that a
	// stolen post-expiry credential cannot lurk for a workday.
	agentCertificateRecoveryGraceWindow = 2 * time.Hour
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
	// AttemptID is the enrollment.Recorder attempt id opened on the panel
	// side for this bootstrap request. The agent echoes it back in
	// subsequent ReportEnrollmentSteps gRPC calls so locally-buffered
	// agent-side events line up with the panel timeline (Task 20).
	// Empty when the panel has no enrollment recorder wired (test fixtures).
	AttemptID string `json:"attempt_id,omitempty"`
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

// resolveEnrollmentFleetGroupID maps an operator-supplied fleet group
// reference (empty / id / friendly name) to the canonical UUID.
// Returns (id, true) on success; on failure it writes the appropriate
// HTTP error and returns ("", false).
func (s *Server) resolveEnrollmentFleetGroupID(w http.ResponseWriter, r *http.Request, fleetGroupID string) (string, bool) {
	if fleetGroupID == "" {
		defaultGroup, err := s.fleetSvc.EnsureDefault(r.Context())
		if err != nil {
			s.logger.Error("ensure default fleet group failed", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return "", false
		}
		return defaultGroup.ID, true
	}
	resolved, err := s.fleetSvc.Get(r.Context(), fleetGroupID)
	if errors.Is(err, storage.ErrNotFound) {
		resolved, err = s.fleetSvc.GetByName(r.Context(), fleetGroupID)
	}
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "fleet group not found")
			return "", false
		}
		s.logger.Error("lookup fleet group for enrollment failed", "fleet_group_id", fleetGroupID, "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return "", false
	}
	return resolved.ID, true
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

		fleetGroupID, ok := s.resolveAndAuthorizeEnrollmentScope(w, r, user, request.FleetGroupID)
		if !ok {
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
			writeError(w, http.StatusInternalServerError, msgInternalError)
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

// resolveAndAuthorizeEnrollmentScope resolves the fleet-group reference and
// then verifies the operator's R-S-14 scope. Empty input → seed/use the
// default group. Otherwise tries id first, then falls back to the unique
// name so operators/scripts can use the friendly slug without hunting for
// UUIDs. Done before the scope check so name → id translation runs first
// and the operator gets the same UX even when scoped.
func (s *Server) resolveAndAuthorizeEnrollmentScope(w http.ResponseWriter, r *http.Request, user auth.User, requested string) (string, bool) {
	fleetGroupID, ok := s.resolveEnrollmentFleetGroupID(w, r, requested)
	if !ok {
		return "", false
	}
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return "", false
	}
	if !scope.IsAllowed(fleetGroupID) {
		writeError(w, http.StatusForbidden, "fleet group outside operator scope")
		return "", false
	}
	return fleetGroupID, true
}

func (s *Server) handleAgentBootstrap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Task 13: open the enrollment attempt FIRST so even an early
		// rejection (missing header, malformed body) shows up in the
		// timeline. The token id is not known yet — leave it blank and
		// the agent id is attached after enrollAgent returns.
		//
		// enrollmentRec is nil when no SQL-backed store is wired (test
		// fixtures with in-memory mocks); in that case every Recorder
		// call below is short-circuited so the legacy behaviour is
		// unchanged.
		var attemptID string
		if s.enrollmentRec != nil {
			id, err := s.enrollmentRec.Begin(ctx, enrollment.ModeInbound, "", r.RemoteAddr)
			if err != nil {
				s.logger.Error("begin enrollment attempt", "error", err)
				// Begin failure must not block the handler; carry on
				// without timeline recording so production traffic still
				// flows even if the enrollment_attempts table is unhappy.
			} else {
				attemptID = id
				s.enrollmentRec.Event(ctx, attemptID, enrollment.StepBootstrapRequestReceived, enrollment.LevelInfo, "bootstrap request received", nil)
			}
		}

		token, ok := bearerTokenFromHeader(r.Header.Get("Authorization"))
		if !ok {
			if s.enrollmentRec != nil && attemptID != "" {
				s.mapAndFailEnrollment(ctx, attemptID, &enrollmentError{
					code:   enrollment.ErrTokenNotFound,
					fields: map[string]any{"reason": "missing Authorization header"},
				})
			}
			writeError(w, http.StatusUnauthorized, "missing bootstrap token")
			return
		}

		var request agentBootstrapRequest
		if err := decodeJSON(r, &request); err != nil {
			if s.enrollmentRec != nil && attemptID != "" {
				s.mapAndFailEnrollment(ctx, attemptID, &enrollmentError{
					code:  enrollment.ErrCSRInvalid,
					cause: err,
				})
			}
			writeError(w, http.StatusBadRequest, "invalid bootstrap payload")
			return
		}
		if request.NodeName == "" || request.Version == "" {
			if s.enrollmentRec != nil && attemptID != "" {
				s.mapAndFailEnrollment(ctx, attemptID, &enrollmentError{
					code:   enrollment.ErrCSRInvalid,
					fields: map[string]any{"reason": "missing node_name or version"},
				})
			}
			writeError(w, http.StatusBadRequest, "node_name and version are required")
			return
		}

		response, err := s.enrollAgent(ctx, agentEnrollmentRequest{
			Token:     token,
			NodeName:  request.NodeName,
			Version:   request.Version,
			AttemptID: attemptID,
		}, s.now())
		if err != nil {
			if s.enrollmentRec != nil && attemptID != "" {
				s.mapAndFailEnrollment(ctx, attemptID, err)
			}
			// Preserve the existing status-code mapping verbatim — operator
			// automation reads on these specific codes/strings.
			switch {
			case errors.Is(err, security.ErrEnrollmentTokenInvalid),
				errors.Is(err, security.ErrEnrollmentTokenConsumed),
				errors.Is(err, security.ErrEnrollmentTokenExpired),
				errors.Is(err, errEnrollmentTokenRevoked):
				writeError(w, http.StatusForbidden, err.Error())
			default:
				s.logger.Error("agent bootstrap failed", "node_name", request.NodeName, "error", err)
				writeError(w, http.StatusInternalServerError, msgInternalError)
			}
			return
		}

		if s.enrollmentRec != nil && attemptID != "" {
			if attachErr := s.enrollmentRec.AttachAgent(ctx, attemptID, response.AgentID); attachErr != nil {
				s.logger.Warn("attach agent to enrollment attempt", "attempt_id", attemptID, "agent_id", response.AgentID, "error", attachErr)
			}
			s.enrollmentRec.Event(ctx, attemptID, enrollment.StepCertReturned, enrollment.LevelInfo, "cert returned", nil)
			if completeErr := s.enrollmentRec.Complete(ctx, attemptID); completeErr != nil {
				s.logger.Warn("complete enrollment attempt", "attempt_id", attemptID, "error", completeErr)
			}
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
			AttemptID:      attemptID,
		})
	}
}

func (s *Server) handleListEnrollmentTokens() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, ok := s.requireEnrollmentManager(w, r)
		if !ok {
			return
		}

		tokens, err := s.listEnrollmentTokensWithContext(r.Context(), s.now(), r.URL, s.trustedForwardedProto(r), r.Host)
		if err != nil {
			s.logger.Error("list enrollment tokens failed", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		// R-S-14: filter by fleet-group scope.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		tokens = filterEnrollmentTokensByScope(tokens, scope)

		writeJSON(w, http.StatusOK, tokens)
	}
}

func filterEnrollmentTokensByScope(tokens []enrollmentTokenResponse, scope FleetScopeAccess) []enrollmentTokenResponse {
	if scope.Global {
		return tokens
	}
	filtered := tokens[:0]
	for _, t := range tokens {
		if scope.IsAllowed(t.FleetGroupID) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (s *Server) handleRevokeEnrollmentToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, ok := s.requireEnrollmentManager(w, r)
		if !ok {
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

		if !s.authorizeEnrollmentTokenRevoke(w, r, user, value) {
			return
		}

		s.executeEnrollmentTokenRevoke(w, r, session, value)
	}
}

func (s *Server) executeEnrollmentTokenRevoke(w http.ResponseWriter, r *http.Request, session auth.Session, value string) {
	revoked, changed, err := s.revokeEnrollmentTokenWithContext(r.Context(), value, s.now())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgEnrollmentTokenNotFound)
			return
		}
		s.logger.Error("revoke enrollment token failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return
	}
	if changed {
		s.appendAuditWithContext(r.Context(), session.UserID, "agents.enrollment.revoke", maskToken(revoked.Value), map[string]any{
			"fleet_group_id": revoked.FleetGroupID,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// requireEnrollmentManager handles the session + role + persistent-store
// preconditions shared by every enrollment-token mutation handler.
func (s *Server) requireEnrollmentManager(w http.ResponseWriter, r *http.Request) (auth.Session, auth.User, bool) {
	session, user, err := s.requireSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Session{}, auth.User{}, false
	}
	if user.Role == auth.RoleViewer {
		writeError(w, http.StatusForbidden, "viewer role cannot manage enrollment tokens")
		return auth.Session{}, auth.User{}, false
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "persistent store required")
		return auth.Session{}, auth.User{}, false
	}
	return session, user, true
}

// authorizeEnrollmentTokenRevoke loads the durable record and verifies it
// sits within the operator's fleet scope (R-S-14). 404 is returned for
// out-of-scope or missing tokens to avoid leaking existence.
func (s *Server) authorizeEnrollmentTokenRevoke(w http.ResponseWriter, r *http.Request, user auth.User, value string) bool {
	existing, lookupErr := s.store.GetEnrollmentToken(r.Context(), value)
	if lookupErr != nil {
		if errors.Is(lookupErr, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgEnrollmentTokenNotFound)
			return false
		}
		s.logger.Error("lookup enrollment token failed", "error", lookupErr)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return false
	}
	if !scope.IsAllowed(existing.FleetGroupID) {
		writeError(w, http.StatusNotFound, msgEnrollmentTokenNotFound)
		return false
	}
	return true
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

// resolveConsumeConflict figures out the precise security error to surface
// when ConsumeEnrollmentToken returned ErrConflict — the row could have
// been consumed concurrently or revoked in the same window.
func (s *Server) resolveConsumeConflict(ctx context.Context, value string) error {
	latest, latestErr := s.store.GetEnrollmentToken(ctx, value)
	if latestErr != nil {
		if errors.Is(latestErr, storage.ErrNotFound) {
			return security.ErrEnrollmentTokenInvalid
		}
		return latestErr
	}
	if latest.RevokedAt != nil {
		return errEnrollmentTokenRevoked
	}
	return security.ErrEnrollmentTokenConsumed
}

func tokenConsumePreCheck(token storage.EnrollmentTokenRecord, now time.Time) error {
	if token.ConsumedAt != nil {
		return security.ErrEnrollmentTokenConsumed
	}
	if token.RevokedAt != nil {
		return errEnrollmentTokenRevoked
	}
	if now.UTC().After(token.ExpiresAt.UTC()) {
		return security.ErrEnrollmentTokenExpired
	}
	return nil
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
	if err := tokenConsumePreCheck(token, now); err != nil {
		return security.EnrollmentToken{}, err
	}

	consumed, err := s.store.ConsumeEnrollmentToken(ctx, value, now.UTC())
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return security.EnrollmentToken{}, s.resolveConsumeConflict(ctx, value)
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
		return settings.GRPCPublicEndpoint, defaultGatewayHost
	}

	return defaultBootstrapGatewayAddress(host)
}

func defaultBootstrapGatewayAddress(host string) (string, string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "127.0.0.1:8443", defaultGatewayHost
	}

	serverName := defaultGatewayHost
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
