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
			s.logger.Error("agent certificate recovery grant lookup failed", "agent_id", request.AgentID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if agentCertificateRecoveryGrantStatus(grant, now) != "allowed" {
			writeError(w, http.StatusForbidden, "agent certificate recovery is not allowed")
			return
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
		if _, err := s.store.UseAgentCertificateRecoveryGrant(r.Context(), request.AgentID, now); err != nil {
			if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
				writeError(w, http.StatusForbidden, "agent certificate recovery is not allowed")
				return
			}
			s.logger.Error("agent certificate recovery grant consume failed", "agent_id", request.AgentID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		issued, err := s.authority.issueClientCertificate(request.AgentID, now)
		if err != nil {
			s.logger.Error("agent certificate recovery cert issue failed after grant consume", "agent_id", request.AgentID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error; please recreate recovery grant")
			return
		}

		s.appendAuditWithContext(r.Context(), request.AgentID, "agents.certificate.recovered", request.AgentID, map[string]any{
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
			s.logger.Error("create certificate recovery grant failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
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
			s.logger.Error("revoke certificate recovery grant failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
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
