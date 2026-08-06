package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// telemtUpdateStrategyPayload is the wire shape of an
// updates.Strategy — used both as the PUT request body and nested in the
// GET response.
type telemtUpdateStrategyPayload struct {
	Mode        string `json:"mode"`
	RestartSpec string `json:"restart_spec"`
	BinaryPath  string `json:"binary_path"`
	AssetFlavor string `json:"asset_flavor"`
}

// telemtUpdateStrategyResponse is the GET response shape: the persisted
// strategy (nil when the agent has never had one configured) plus the
// agent's live probe (nil until the agent has sent at least one snapshot
// carrying it — see api.TelemtUpdateProbe / Task 7).
type telemtUpdateStrategyResponse struct {
	Strategy *telemtUpdateStrategyPayload `json:"strategy"`
	Probe    *TelemtUpdateProbe           `json:"probe"`
}

// handleGetTelemtUpdateStrategy returns the persisted Telemt update
// strategy for an agent (null when unconfigured) alongside its live probe,
// so the dashboard can render "not configured yet, but here's what we
// detected" without a second round trip.
func (s *Server) handleGetTelemtUpdateStrategy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id is required")
			return
		}
		agent, exists := s.live.Get(agentID)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		st, ok, err := s.telemtStrategySvc.Get(r.Context(), agentID)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "failed to read update strategy", err)
			return
		}

		resp := telemtUpdateStrategyResponse{Probe: agent.TelemtUpdateProbe}
		if ok {
			resp.Strategy = &telemtUpdateStrategyPayload{
				Mode:        st.Mode,
				RestartSpec: st.RestartSpec,
				BinaryPath:  st.BinaryPath,
				AssetFlavor: st.AssetFlavor,
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handlePutTelemtUpdateStrategy validates and persists the Telemt update
// strategy for an agent. Admin-only (see routes.go) — the strategy governs
// which local command the panel will later run to restart telemt after a
// binary swap, so it carries the same trust bar as the panel/agent update
// settings it complements.
func (s *Server) handlePutTelemtUpdateStrategy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id is required")
			return
		}
		if _, exists := s.live.Get(agentID); !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		var req telemtUpdateStrategyPayload
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		st := updates.Strategy{
			Mode:        req.Mode,
			RestartSpec: req.RestartSpec,
			BinaryPath:  req.BinaryPath,
			AssetFlavor: req.AssetFlavor,
		}
		if err := s.telemtStrategySvc.Put(r.Context(), agentID, st); err != nil {
			if errors.Is(err, updates.ErrInvalidStrategy) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "failed to save update strategy", err)
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.update_strategy.set", agentID, map[string]any{
			"mode": st.Mode,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}
