package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/security"
)

var errEnrollmentTokenRevoked = errors.New("enrollment token revoked")

const (
	agentCertificateRecoveryProofSkew  = 5 * time.Minute
	agentCertificateRecoveryGraceWindow = 7 * 24 * time.Hour
	defaultAgentCertificateRecoveryGrantTTL = 15 * time.Minute
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

		token, err := s.issueEnrollmentToken(security.EnrollmentScope{
			FleetGroupID: request.FleetGroupID,
			TTL:          time.Duration(request.TTLSeconds) * time.Second,
		}, s.now())
		if err != nil {
			log.Printf("create enrollment token failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAudit(session.UserID, "agents.enrollment.create", token.Value, map[string]any{
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
				log.Printf("agent bootstrap failed for node %q: %v", request.NodeName, err)
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

func (s *Server) handleAgentCertificateRecovery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "persistent store required")
			return
		}

		var request agentCertificateRecoveryRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery payload")
			return
		}
		if strings.TrimSpace(request.AgentID) == "" || strings.TrimSpace(request.CertificatePEM) == "" || strings.TrimSpace(request.ProofNonce) == "" || strings.TrimSpace(request.ProofSignature) == "" || request.ProofTimestampUnix == 0 {
			writeError(w, http.StatusBadRequest, "recovery payload is incomplete")
			return
		}

		now := s.now().UTC()
		certificate, err := parseRecoveryCertificate(request.CertificatePEM)
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid recovery certificate")
			return
		}
		if err := verifyRecoveryCertificate(certificate, request.AgentID, s.authority.certificate, now); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		if err := verifyRecoveryProof(certificate, request.AgentID, request.ProofTimestampUnix, request.ProofNonce, request.ProofSignature, now); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}

		s.mu.RLock()
		agent, exists := s.agents[request.AgentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusForbidden, "agent is not enrolled")
			return
		}
		grant, err := s.store.GetAgentCertificateRecoveryGrant(r.Context(), request.AgentID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusForbidden, "agent certificate recovery is not allowed")
				return
			}
			log.Printf("agent certificate recovery grant lookup failed for agent %q: %v", request.AgentID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if agentCertificateRecoveryGrantStatus(grant, now) != "allowed" {
			writeError(w, http.StatusForbidden, "agent certificate recovery is not allowed")
			return
		}

		issued, err := s.authority.issueClientCertificate(request.AgentID, now)
		if err != nil {
			log.Printf("agent certificate recovery failed for agent %q: %v", request.AgentID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if _, err := s.store.UseAgentCertificateRecoveryGrant(r.Context(), request.AgentID, now); err != nil {
			if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
				writeError(w, http.StatusForbidden, "agent certificate recovery is not allowed")
				return
			}
			log.Printf("agent certificate recovery grant consume failed for agent %q: %v", request.AgentID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAudit(request.AgentID, "agents.certificate.recovered", request.AgentID, map[string]any{
			"node_name": agent.NodeName,
		})
		grpcEndpoint, grpcServerName := s.bootstrapGatewayAddress(r.Host)
		writeJSON(w, http.StatusOK, agentBootstrapResponse{
			AgentID:        request.AgentID,
			CertificatePEM: issued.CertificatePEM,
			PrivateKeyPEM:  issued.PrivateKeyPEM,
			CAPEM:          issued.CAPEM,
			GRPCEndpoint:   grpcEndpoint,
			GRPCServerName: grpcServerName,
			ExpiresAtUnix:  issued.ExpiresAt.Unix(),
		})
	}
}

func (s *Server) handleCreateAgentCertificateRecoveryGrant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "persistent store required")
			return
		}

		var request agentCertificateRecoveryGrantRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery grant payload")
			return
		}
		ttl, err := agentCertificateRecoveryGrantTTL(request.TTLSeconds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		agentID := chi.URLParam(r, "id")
		s.mu.RLock()
		agent, exists := s.agents[agentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		now := s.now().UTC()
		grant := storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agentID,
			IssuedBy:  session.UserID,
			IssuedAt:  now,
			ExpiresAt: now.Add(ttl),
		}
		if err := s.store.PutAgentCertificateRecoveryGrant(r.Context(), grant); err != nil {
			log.Printf("create certificate recovery grant failed for agent %q: %v", agentID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAudit(session.UserID, "agents.certificate_recovery.allowed", agentID, map[string]any{
			"node_name":        agent.NodeName,
			"expires_at_unix":  grant.ExpiresAt.Unix(),
			"recovery_ttl_sec": int(ttl / time.Second),
		})
		writeJSON(w, http.StatusCreated, agentCertificateRecoveryGrantResponseFromRecord(grant, now))
	}
}

func (s *Server) handleRevokeAgentCertificateRecoveryGrant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "persistent store required")
			return
		}

		agentID := chi.URLParam(r, "id")
		now := s.now().UTC()
		grant, err := s.store.RevokeAgentCertificateRecoveryGrant(r.Context(), agentID, now)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "agent recovery grant not found")
				return
			}
			log.Printf("revoke certificate recovery grant failed for agent %q: %v", agentID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAudit(session.UserID, "agents.certificate_recovery.revoked", agentID, nil)
		writeJSON(w, http.StatusOK, agentCertificateRecoveryGrantResponseFromRecord(grant, now))
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

		tokens, err := s.listEnrollmentTokens(s.now(), r.URL, r.Header.Get("X-Forwarded-Proto"), r.Host)
		if err != nil {
			log.Printf("list enrollment tokens failed: %v", err)
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

		revoked, changed, err := s.revokeEnrollmentToken(value, s.now())
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "enrollment token not found")
				return
			}
			log.Printf("revoke enrollment token failed for value %q: %v", value, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if changed {
			s.appendAudit(session.UserID, "agents.enrollment.revoke", revoked.Value, map[string]any{
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
		Value:        consumed.Value,
		FleetGroupID: consumed.FleetGroupID,
		IssuedAt:     consumed.IssuedAt.UTC(),
		ExpiresAt:    consumed.ExpiresAt.UTC(),
	}, nil
}

func (s *Server) listEnrollmentTokens(now time.Time, requestURL *url.URL, forwardedProto string, requestHost string) ([]enrollmentTokenResponse, error) {
	records, err := s.store.ListEnrollmentTokens(context.Background())
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

func parseRecoveryCertificate(certificatePEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		return nil, errors.New("failed to decode recovery certificate")
	}

	return x509.ParseCertificate(block.Bytes)
}

func verifyRecoveryCertificate(certificate *x509.Certificate, agentID string, authorityCertificate *x509.Certificate, now time.Time) error {
	if certificate.Subject.CommonName != agentID {
		return errors.New("recovery certificate agent mismatch")
	}
	if authorityCertificate == nil {
		return errors.New("recovery authority is not available")
	}
	if err := certificate.CheckSignatureFrom(authorityCertificate); err != nil {
		return errors.New("recovery certificate is not signed by control-plane authority")
	}
	if certificate.NotBefore.After(now.Add(agentCertificateRecoveryProofSkew)) {
		return errors.New("recovery certificate is not yet valid")
	}
	if now.After(certificate.NotAfter.Add(agentCertificateRecoveryGraceWindow)) {
		return errors.New("recovery certificate grace window expired")
	}

	return nil
}

func verifyRecoveryProof(certificate *x509.Certificate, agentID string, proofTimestampUnix int64, proofNonce string, proofSignature string, now time.Time) error {
	proofTime := time.Unix(proofTimestampUnix, 0).UTC()
	if proofTime.Before(now.Add(-agentCertificateRecoveryProofSkew)) || proofTime.After(now.Add(agentCertificateRecoveryProofSkew)) {
		return errors.New("recovery proof timestamp is out of range")
	}
	if len(proofNonce) < 8 || len(proofNonce) > 128 {
		return errors.New("recovery proof nonce is invalid")
	}

	signature, err := base64.RawURLEncoding.DecodeString(proofSignature)
	if err != nil {
		return errors.New("recovery proof signature is malformed")
	}

	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("recovery certificate key type is unsupported")
	}

	payload := recoveryProofPayload(agentID, proofTimestampUnix, proofNonce)
	digest := sha256.Sum256([]byte(payload))
	if !ecdsa.VerifyASN1(publicKey, digest[:], signature) {
		return errors.New("recovery proof signature is invalid")
	}

	return nil
}

func recoveryProofPayload(agentID string, proofTimestampUnix int64, proofNonce string) string {
	return agentID + "\n" + strconv.FormatInt(proofTimestampUnix, 10) + "\n" + proofNonce
}

func agentCertificateRecoveryGrantTTL(ttlSeconds int) (time.Duration, error) {
	if ttlSeconds < 0 {
		return 0, errors.New("ttl_seconds must be zero or positive")
	}
	if ttlSeconds == 0 {
		return defaultAgentCertificateRecoveryGrantTTL, nil
	}

	return time.Duration(ttlSeconds) * time.Second, nil
}

func agentCertificateRecoveryGrantStatus(grant storage.AgentCertificateRecoveryGrantRecord, now time.Time) string {
	if grant.RevokedAt != nil {
		return "revoked"
	}
	if grant.UsedAt != nil {
		return "used"
	}
	if !grant.ExpiresAt.After(now) {
		return "expired"
	}

	return "allowed"
}

func agentCertificateRecoveryGrantResponseFromRecord(grant storage.AgentCertificateRecoveryGrantRecord, now time.Time) agentCertificateRecoveryGrantResponse {
	return agentCertificateRecoveryGrantResponse{
		AgentID:       grant.AgentID,
		Status:        agentCertificateRecoveryGrantStatus(grant, now),
		IssuedAtUnix:  grant.IssuedAt.Unix(),
		ExpiresAtUnix: grant.ExpiresAt.Unix(),
		UsedAtUnix:    optionalUnix(grant.UsedAt),
		RevokedAtUnix: optionalUnix(grant.RevokedAt),
	}
}

func optionalUnix(value *time.Time) *int64 {
	if value == nil {
		return nil
	}

	unix := value.Unix()
	return &unix
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
