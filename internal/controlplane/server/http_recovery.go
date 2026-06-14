package server

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Server) handleAgentCertificateRecovery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, msgPersistentStoreRequired)
			return
		}

		var request agentCertificateRecoveryRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery payload")
			return
		}
		if !validateRecoveryRequestFields(w, request) {
			return
		}

		now := s.now().UTC()
		if !s.verifyRecoveryRequestCryptography(w, request, now) {
			return
		}

		agent, ok := s.lookupEnrolledAgent(w, request.AgentID)
		if !ok {
			return
		}
		if !s.checkAndConsumeRecoveryGrant(w, r, request.AgentID, now) {
			return
		}
		issued, err := s.authority.issueAgentCertificateFromCSR(request.CSRPEM, request.AgentID, agentCertificateLifetime, true, now)
		if err != nil {
			s.logger.Warn("agent certificate recovery: sign CSR failed", "agent_id", request.AgentID, "error", err)
			writeError(w, http.StatusBadRequest, "invalid recovery csr")
			return
		}

		s.persistAgentCertPin(r.Context(), request.AgentID, issued.CertificatePEM)
		s.appendAuditWithContext(r.Context(), request.AgentID, "agents.certificate.recovered", request.AgentID, map[string]any{
			"node_name": agent.NodeName,
		})
		grpcEndpoint, grpcServerName := s.bootstrapGatewayAddress(r.Host)
		writeJSON(w, http.StatusOK, agentBootstrapResponse{
			AgentID:        request.AgentID,
			CertificatePEM: issued.CertificatePEM,
			CAPEM:          issued.CAPEM,
			GRPCEndpoint:   grpcEndpoint,
			GRPCServerName: grpcServerName,
			ExpiresAtUnix:  issued.ExpiresAt.Unix(),
		})
	}
}

// validateRecoveryRequestFields rejects payloads missing any of the required
// fields. Pulled out so the handler stays linear and the long disjunction
// no longer dominates the function's cognitive complexity score.
func validateRecoveryRequestFields(w http.ResponseWriter, request agentCertificateRecoveryRequest) bool {
	if strings.TrimSpace(request.AgentID) == "" ||
		strings.TrimSpace(request.CertificatePEM) == "" ||
		strings.TrimSpace(request.ProofNonce) == "" ||
		strings.TrimSpace(request.ProofSignature) == "" ||
		strings.TrimSpace(request.CSRPEM) == "" ||
		request.ProofTimestampUnix == 0 {
		writeError(w, http.StatusBadRequest, "recovery payload is incomplete")
		return false
	}
	return true
}

// verifyRecoveryRequestCryptography parses the recovery certificate and
// verifies it (signed by our authority) and the proof signature. Writes
// 403 on any failure.
func (s *Server) verifyRecoveryRequestCryptography(w http.ResponseWriter, request agentCertificateRecoveryRequest, now time.Time) bool {
	certificate, err := parseRecoveryCertificate(request.CertificatePEM)
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid recovery certificate")
		return false
	}
	if err := verifyRecoveryCertificate(certificate, request.AgentID, s.authority.certificate, now); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return false
	}
	if err := verifyRecoveryProof(certificate, request.AgentID, request.ProofTimestampUnix, request.ProofNonce, request.ProofSignature, now); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return false
	}
	return true
}

// lookupEnrolledAgent returns the in-memory snapshot of the named agent
// or writes 403 when no enrollment exists.
func (s *Server) lookupEnrolledAgent(w http.ResponseWriter, agentID string) (Agent, bool) {
	agent, exists := s.live.Get(agentID)
	if !exists {
		writeError(w, http.StatusForbidden, "agent is not enrolled")
		return Agent{}, false
	}
	return agent, true
}

// checkAndConsumeRecoveryGrant verifies an "allowed" grant exists for this
// agent and consumes it atomically (P1-SEC-07). Writes the appropriate HTTP
// error on any failure path. Returns true only if the consume succeeded so
// the caller may proceed to issue the new certificate.
func (s *Server) checkAndConsumeRecoveryGrant(w http.ResponseWriter, r *http.Request, agentID string, now time.Time) bool {
	grant, err := s.store.GetAgentCertificateRecoveryGrant(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusForbidden, msgRecoveryNotAllowed)
			return false
		}
		s.logger.Error("agent certificate recovery grant lookup failed", "agent_id", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	if agentCertificateRecoveryGrantStatus(grant, now) != "allowed" {
		writeError(w, http.StatusForbidden, msgRecoveryNotAllowed)
		return false
	}

	// P1-SEC-07: consume the grant BEFORE issuing the certificate so a
	// crash between cert issue and grant consume cannot leave the grant
	// reusable while a fresh cert is already minted. The grant consume
	// is an atomic SQL UPDATE — concurrent recovery requests race here
	// and only the winner proceeds; losers see ErrConflict and fail
	// closed.
	//
	// Trade-off: if cert issuance itself fails after consume, the grant
	// is lost and the admin must create a new one. This is acceptable
	// because issuance is in-process and only fails on impossible
	// conditions (CA unloaded, entropy exhausted) — a much narrower
	// failure window than the former TOCTOU.
	if _, err := s.store.UseAgentCertificateRecoveryGrant(r.Context(), agentID, now); err != nil {
		if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
			writeError(w, http.StatusForbidden, msgRecoveryNotAllowed)
			return false
		}
		s.logger.Error("agent certificate recovery grant consume failed", "agent_id", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	return true
}

func (s *Server) handleCreateAgentCertificateRecoveryGrant() http.HandlerFunc {
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
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, msgPersistentStoreRequired)
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
		agent, exists := s.live.Get(agentID)
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
			s.logger.Error("create certificate recovery grant failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.certificate_recovery.allowed", agentID, map[string]any{
			"node_name":        agent.NodeName,
			"expires_at_unix":  grant.ExpiresAt.Unix(),
			"recovery_ttl_sec": int(ttl / time.Second),
		})
		writeJSON(w, http.StatusCreated, agentCertificateRecoveryGrantResponseFromRecord(grant, now))
	}
}

func (s *Server) handleRevokeAgentCertificateRecoveryGrant() http.HandlerFunc {
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
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, msgPersistentStoreRequired)
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
			s.logger.Error("revoke certificate recovery grant failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.certificate_recovery.revoked", agentID, nil)
		writeJSON(w, http.StatusOK, agentCertificateRecoveryGrantResponseFromRecord(grant, now))
	}
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

	ttl := time.Duration(ttlSeconds) * time.Second
	if ttl > maxAgentCertificateRecoveryGrantTTL {
		return 0, errors.New("ttl_seconds exceeds the maximum recovery window")
	}

	return ttl, nil
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
