package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleClientRotation is the shared skeleton of the two client-credential
// rotation endpoints (secret / subscription token): scope check -> mutation
// scope check -> rotate -> log+audit -> full detail response (P5, audit #20).
func (s *Server) handleClientRotation(
	rotate func(ctx context.Context, clientID, actorID string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error),
	logMsg, auditAction string,
	showSecret bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := rotate(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), logMsg, "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, auditAction, string(client.ID), nil)
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, showSecret))
	}
}

func (s *Server) handleRotateSubscriptionToken() http.HandlerFunc {
	return s.handleClientRotation(s.rotateSubscriptionToken, "client subscription token rotated", "clients.rotate_subscription", false)
}
