package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleRotateSubscriptionToken() http.HandlerFunc {
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

		if !s.ensureClientMutationScope(w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := s.rotateSubscriptionToken(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.Info("client subscription token rotated", "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.rotate_subscription", string(client.ID), nil)
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false))
	}
}
