package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type createJobRequest struct {
	Action         string   `json:"action"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	IdempotencyKey string   `json:"idempotency_key"`
	TTLSeconds     int      `json:"ttl_seconds"`
}

func (s *Server) handleJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusUnauthorized, "unauthorized", err)
			return
		}
		// R-S-14: load the operator's scope so the response can be
		// narrowed to jobs whose targets sit inside their visible
		// fleet groups.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		// S25 T1: opt-in keyset pagination. Presence of the ?cursor=
		// query param (even empty, == first page) routes through the
		// store-backed cursor path; legacy callers get the in-memory
		// jobs.ListRecentWithContext snapshot unchanged. The two paths
		// intentionally differ: the cursor path goes to disk and can
		// reach beyond the in-memory ring; the legacy path is faster
		// but bounded by jobs.DefaultListRecentLimit.
		if r.URL.Query().Has("cursor") {
			s.handleJobsCursor(w, r, scope)
			return
		}

		listed := s.jobs.ListRecentWithContext(r.Context(), parseListLimit(r))
		listed = s.filterJobsByScope(listed, scope)
		// Q2.U-S-07: redact PayloadJSON for ALL roles in the list
		// endpoint. Mutating jobs (rollout_client_config, rotate_secret)
		// embed client secrets in the payload; admins and operators do
		// not need them for routine browsing. Internal dispatch keeps
		// the payload via grpc_gateway.go and the per-action handlers
		// in clients_flow.go which read job.PayloadJSON directly from
		// the in-memory store, never from the HTTP response.
		for i := range listed {
			listed[i].PayloadJSON = ""
		}
		writeJSON(w, http.StatusOK, listed)
	}
}

// jobsCursorResponse is the wire shape returned by handleJobsCursor. Items
// match the redacted-payload row shape used by the legacy /api/jobs response;
// next_cursor is the opaque base64-url string a client passes back as
// ?cursor= to fetch the next page. Empty string means "no more pages".
type jobsCursorResponse struct {
	Items      []jobs.Job `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

// handleJobsCursor serves the cursor-paginated branch of /api/jobs. Falls
// back to writeError for any decoding failure so a stale-but-valid-looking
// cursor surfaces as 400 rather than silently restarting the page walk.
func (s *Server) handleJobsCursor(w http.ResponseWriter, r *http.Request, scope FleetScopeAccess) {
	if s.store == nil {
		// No store wired (in-memory test fixtures). Return an empty
		// page rather than 500 — the caller can opt back into the
		// legacy endpoint by dropping the cursor param.
		writeJSON(w, http.StatusOK, jobsCursorResponse{Items: []jobs.Job{}})
		return
	}
	createdAt, afterID, err := storage.DecodeKeysetCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid cursor", err)
		return
	}
	limit := parseCursorLimit(r)
	records, next, err := s.store.ListJobsCursor(r.Context(), storage.ListJobsCursorParams{
		Limit:          limit,
		AfterCreatedAt: createdAt,
		AfterID:        afterID,
	})
	if err != nil {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "list jobs failed", err)
		return
	}
	items := make([]jobs.Job, 0, len(records))
	for _, rec := range records {
		job := jobs.JobFromRecord(rec)
		if !scope.Global {
			if !s.jobHasInScopeTarget(job, scope) {
				continue
			}
		}
		// Same secret-hygiene contract as the legacy branch — never
		// expose PayloadJSON in the list response.
		job.PayloadJSON = ""
		items = append(items, job)
	}
	writeJSON(w, http.StatusOK, jobsCursorResponse{
		Items:      items,
		NextCursor: storage.EncodeKeysetCursor(next.AfterCreatedAt, next.AfterID),
	})
}

// parseCursorLimit reads ?limit= for the cursor branch and applies the
// DefaultCursorPageSize / MaxCursorPageSize envelope. A missing or invalid
// value falls back to the default; values above the cap are clamped.
func parseCursorLimit(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return storage.DefaultCursorPageSize
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return storage.DefaultCursorPageSize
	}
	return storage.NormalizeCursorLimit(parsed)
}

// parseListLimit returns the operator-supplied ?limit= when present and
// positive, falling back to jobs.DefaultListRecentLimit. The store
// applies its own absolute cap, so this only relaxes between the
// default and the cap (Q2.U-P-13).
func parseListLimit(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return jobs.DefaultListRecentLimit
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return jobs.DefaultListRecentLimit
	}
	return parsed
}

// filterJobsByScope drops jobs whose target agents are entirely outside
// the operator's scope. Global scope is a no-op shortcut so the lock
// is only taken on the constrained path.
func (s *Server) filterJobsByScope(listed []jobs.Job, scope FleetScopeAccess) []jobs.Job {
	if scope.Global {
		return listed
	}
	filtered := listed[:0]
	for _, job := range listed {
		if s.jobHasInScopeTarget(job, scope) {
			filtered = append(filtered, job)
		}
	}
	return filtered
}

func (s *Server) jobHasInScopeTarget(job jobs.Job, scope FleetScopeAccess) bool {
	for _, agentID := range job.TargetAgentIDs {
		agent, ok := s.live.Get(agentID)
		if ok && scope.IsAllowed(agent.FleetGroupID) {
			return true
		}
	}
	return false
}

// targetsInScope reports whether every target agent is reachable for
// the operator. Global scope short-circuits the lookup.
func (s *Server) targetsInScope(targetIDs []string, scope FleetScopeAccess) bool {
	if scope.Global {
		return true
	}
	for _, agentID := range targetIDs {
		agent, ok := s.live.Get(agentID)
		if !ok || !scope.IsAllowed(agent.FleetGroupID) {
			return false
		}
	}
	return true
}

// readOnlyAgents snapshots the ReadOnly flag for each requested target
// so the jobs service can refuse mutating actions against read-only
// agents without re-reading agent state.
func (s *Server) readOnlyAgents(targetIDs []string) map[string]bool {
	out := make(map[string]bool, len(targetIDs))
	for _, agentID := range targetIDs {
		if agent, ok := s.live.Get(agentID); ok {
			out[agentID] = agent.ReadOnly
		}
	}
	return out
}

// createJobActionAllowed gates which actions the generic POST /api/jobs
// endpoint may enqueue (wire-audit I1). createJobRequest has no payload_json
// field, so any action whose agent handler decodes a payload used to be
// accepted here, dispatched, and then failed on EVERY agent with a JSON
// decode error — nothing warned at enqueue time.
//
//   - telemetry.refresh_diagnostics: payload-free on the agent (agent.go
//     HandleJob decodes nothing) → dispatchable as-is.
//   - agent.self-update: REQUIRES {version, release_base_url}; the panel
//     builds it server-side (selfUpdateJobPayloadForCreate) so the
//     dashboard's bulk self-update button keeps working.
//   - client.create/update/delete/rotate_secret, client.reset_quota,
//     config.apply, switch_transport_mode: payload-carrying, enqueued
//     exclusively by their dedicated panel flows which construct the
//     payload (clients flow, config-apply handler, transport-mode PUT).
func createJobActionAllowed(action jobs.Action) bool {
	switch action {
	case jobs.ActionTelemetryRefreshDiagnostics,
		jobs.ActionAgentSelfUpdate:
		return true
	default:
		return false
	}
}

// validateCreateJobRequest gates the operator's createJob payload
// against role, scope, and action validity. Returns (scope, true)
// only when all checks pass; on failure it writes the appropriate
// HTTP error.
func (s *Server) validateCreateJobRequest(w http.ResponseWriter, r *http.Request, user auth.User, request createJobRequest) (FleetScopeAccess, bool) {
	if user.Role == auth.RoleViewer {
		writeError(w, http.StatusForbidden, "viewer role cannot create jobs")
		return FleetScopeAccess{}, false
	}
	if !jobs.IsValidAction(jobs.Action(request.Action)) {
		writeError(w, http.StatusBadRequest, "unknown job action")
		return FleetScopeAccess{}, false
	}
	if !createJobActionAllowed(jobs.Action(request.Action)) {
		writeError(w, http.StatusBadRequest, "action carries a payload this endpoint cannot supply; use its dedicated endpoint")
		return FleetScopeAccess{}, false
	}
	// R-S-14: every target agent must sit inside the operator's scope.
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return FleetScopeAccess{}, false
	}
	if !s.targetsInScope(request.TargetAgentIDs, scope) {
		writeError(w, http.StatusForbidden, "target agent outside operator scope")
		return FleetScopeAccess{}, false
	}
	return scope, true
}

// selfUpdateJobPayloadForCreate assembles the agent.self-update payload for
// the generic jobs endpoint, resolving the target version (cached latest)
// and repo exactly like the dedicated per-agent update endpoint
// (handleAgentUpdate). Writes the HTTP error and returns ok=false when no
// version is known or the configured repo is invalid — an honest 400 at
// enqueue time instead of a job that fails on every agent.
func (s *Server) selfUpdateJobPayloadForCreate(ctx context.Context, w http.ResponseWriter) (string, bool) {
	s.settingsMu.RLock()
	state := s.updateState
	settings := s.updateSettings
	s.settingsMu.RUnlock()

	targetVersion, ok := resolveAgentTargetVersion(w, "", state)
	if !ok {
		return "", false
	}
	if err := validateGitHubRepo(settings.GitHubRepo); err != nil {
		writeError(w, http.StatusBadRequest, "invalid github_repo configured")
		return "", false
	}
	payloadJSON, err := buildAgentDirectUpdatePayload(settings.GitHubRepo, targetVersion)
	if err != nil {
		s.logger.ErrorContext(ctx, "create job: build self-update payload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to build update payload")
		return "", false
	}
	return string(payloadJSON), true
}

// writeCreateJobError maps Enqueue errors to HTTP status codes.
func writeCreateJobError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrDuplicateIdempotencyKey):
		writeErrorLogged(ctx, w, http.StatusConflict, err.Error(), err)
	case errors.Is(err, jobs.ErrReadOnlyTarget):
		writeErrorLogged(ctx, w, http.StatusConflict, err.Error(), err)
	default:
		writeErrorLogged(ctx, w, http.StatusBadRequest, err.Error(), err)
	}
}

func (s *Server) handleCreateJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusUnauthorized, "unauthorized", err)
			return
		}

		var request createJobRequest
		if err := decodeJSON(r, &request); err != nil {
			writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid job payload", err)
			return
		}

		if _, ok := s.validateCreateJobRequest(w, r, user, request); !ok {
			return
		}

		// agent.self-update is the one allowed action that requires a
		// payload; build it here so the job dispatched from this endpoint
		// carries what the agent needs (see createJobActionAllowed).
		payloadJSON := ""
		if jobs.Action(request.Action) == jobs.ActionAgentSelfUpdate {
			var ok bool
			payloadJSON, ok = s.selfUpdateJobPayloadForCreate(r.Context(), w)
			if !ok {
				return
			}
		}

		readOnlyAgents := s.readOnlyAgents(request.TargetAgentIDs)

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
			Action:         jobs.Action(request.Action),
			TargetAgentIDs: request.TargetAgentIDs,
			TTL:            time.Duration(request.TTLSeconds) * time.Second,
			IdempotencyKey: request.IdempotencyKey,
			ActorID:        session.UserID,
			ReadOnlyAgents: readOnlyAgents,
			PayloadJSON:    payloadJSON,
		}, s.now())
		if err != nil {
			writeCreateJobError(r.Context(), w, err)
			return
		}
		s.notifyAgentSessions(job.TargetAgentIDs)

		s.appendAuditWithContext(r.Context(), session.UserID, "jobs.create", job.ID, map[string]any{
			"action":            request.Action,
			"target_agent_ids":  request.TargetAgentIDs,
			"idempotency_key":   request.IdempotencyKey,
			"requested_role":    user.Role,
			"requested_ttl_sec": request.TTLSeconds,
		})
		s.publishJobCreated(job)
		// L-2: redact PayloadJSON before returning, consistent with the
		// /api/jobs list endpoints. Operator-created jobs carry no secret
		// today, but centralising the scrub keeps a future secret-bearing
		// action from leaking through this direct-response path.
		job.PayloadJSON = ""
		writeJSON(w, http.StatusAccepted, job)
	}
}
