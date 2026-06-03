package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type updateAgentTransportRequest struct {
	TransportMode string `json:"transport_mode"`
	// DialAddress is the public host:port the panel dials to reach the agent
	// (e.g. "vps.example.com:8443"). Required for outbound mode.
	DialAddress string `json:"dial_address,omitempty"`
	// ListenAddress is the local bind spec the agent uses for net.Listen
	// (e.g. ":8443" or "0.0.0.0:8443"). Optional: when empty for outbound,
	// it defaults to ":<port>" derived from DialAddress — necessary on
	// NAT'd VMs (cloud Elastic IPs) where the public host doesn't resolve
	// to a local interface and binding to it returns EADDRNOTAVAIL.
	ListenAddress string `json:"listen_address,omitempty"`
}

// handleUpdateAgentTransportMode handles PUT /agents/{id}/transport-mode.
// It updates the agent's transport_mode + dial_address in the DB, enqueues a
// switch_transport_mode job for the agent, and notifies agenttransport.Manager
// to (de)spawn outbound supervisors accordingly.
func (s *Server) handleUpdateAgentTransportMode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		var req updateAgentTransportRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		req.TransportMode = strings.TrimSpace(req.TransportMode)
		req.DialAddress = strings.TrimSpace(req.DialAddress)
		req.ListenAddress = strings.TrimSpace(req.ListenAddress)

		if req.TransportMode != "inbound" && req.TransportMode != "outbound" {
			writeError(w, http.StatusBadRequest, "transport_mode must be inbound or outbound")
			return
		}
		if req.TransportMode == "outbound" && req.DialAddress == "" {
			writeError(w, http.StatusBadRequest, "dial_address required for outbound mode")
			return
		}
		// Derive the agent-side listen spec from dial_address if the operator
		// did not supply an explicit one. The public dial_address ("vps:8443")
		// is what the panel dials; the agent must bind to a local interface
		// (":8443"). Defaulting to ":<port>" is the right thing on the common
		// AWS/GCP/Azure deployment with a NAT'd public IP.
		listenBind := req.ListenAddress
		if req.TransportMode == "outbound" && listenBind == "" {
			_, port, splitErr := net.SplitHostPort(req.DialAddress)
			if splitErr != nil {
				writeError(w, http.StatusBadRequest, "dial_address must be host:port")
				return
			}
			listenBind = ":" + port
		}

		// Verify the agent exists in memory and the caller can reach it.
		existing, exists := s.live.Get(agentID)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		// Persist to storage.
		if s.store != nil {
			dialAddr := req.DialAddress
			if req.TransportMode == "inbound" {
				dialAddr = ""
			}
			if err := s.store.UpdateAgentTransportMode(r.Context(), agentID, req.TransportMode, dialAddr); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeError(w, http.StatusNotFound, msgAgentNotFound)
					return
				}
				s.logger.Error("update agent transport mode in store failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgStorageError)
				return
			}
		}

		// Map DB transport_mode to agent-level naming for the job payload.
		// DB "inbound"  → agent "dial"   (agent dials the panel)
		// DB "outbound" → agent "listen" (agent listens; panel dials it)
		agentMode := "dial"
		listenAddr := ""
		if req.TransportMode == "outbound" {
			agentMode = "listen"
			listenAddr = listenBind
		}

		jobPayload, _ := json.Marshal(map[string]string{
			"mode":        agentMode,
			"listen_addr": listenAddr,
		})

		var idempotencyKey [16]byte
		_, _ = rand.Read(idempotencyKey[:])

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
			Action:         jobs.ActionSwitchTransportMode,
			TargetAgentIDs: []string{agentID},
			IdempotencyKey: hex.EncodeToString(idempotencyKey[:]),
			ActorID:        session.UserID,
			ReadOnlyAgents: s.readOnlyAgents([]string{agentID}),
			PayloadJSON:    string(jobPayload),
		}, s.now())
		if err != nil {
			s.logger.Error("enqueue switch_transport_mode job failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to enqueue transport mode switch job")
			return
		}
		s.notifyAgentSessions([]string{agentID})

		// Notify the transport manager so outbound supervisors are
		// spawned or torn down immediately (best-effort: no error if nil).
		s.notifyTransportManager(r.Context(), agentID)

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.update_transport_mode", agentID, map[string]any{
			"transport_mode": req.TransportMode,
			"dial_address":   req.DialAddress,
			"listen_address": listenBind,
			"job_id":         job.ID,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}
