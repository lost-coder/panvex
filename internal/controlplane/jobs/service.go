package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

var (
	// ErrDuplicateIdempotencyKey reports a repeated command submission for the same key.
	ErrDuplicateIdempotencyKey = errors.New("duplicate job idempotency key")
	// ErrReadOnlyTarget reports a mutating action aimed at a read-only Telemt instance.
	ErrReadOnlyTarget = errors.New("job targets read-only telemt instance")
)

// Action identifies the normalized control-plane action to execute on a target.
type Action string

const (
	// ActionRuntimeReload reloads the runtime configuration through Telemt.
	ActionRuntimeReload Action = "runtime.reload"
	// ActionUsersCreate creates a Telemt operator account.
	ActionUsersCreate Action = "users.create"
	// ActionClientCreate creates one centrally managed Telemt client on the target node.
	ActionClientCreate Action = "client.create"
	// ActionClientUpdate updates one centrally managed Telemt client on the target node.
	ActionClientUpdate Action = "client.update"
	// ActionClientDelete removes one centrally managed Telemt client from the target node.
	ActionClientDelete Action = "client.delete"
	// ActionClientRotateSecret rotates the managed Telemt client secret on the target node.
	ActionClientRotateSecret Action = "client.rotate_secret"
	// ActionClientResetQuota resets the data-quota counter (used_bytes) for
	// one centrally managed Telemt client on the target node. Hits Telemt's
	// POST /v1/users/{u}/reset-quota (Telemt ≥ 3.4.6). Agents on older
	// Telemt versions report the job as failed with a typed reason so the
	// panel can surface "Reset unavailable (Telemt < 3.4.6)" on the affected
	// deployment row without halting fan-out on other agents.
	ActionClientResetQuota Action = "client.reset_quota"
	// ActionTelemetryRefreshDiagnostics forces a fresh slow-diagnostics pull on the target node.
	ActionTelemetryRefreshDiagnostics Action = "telemetry.refresh_diagnostics"
	// ActionAgentSelfUpdate instructs the agent to download and replace its own binary.
	ActionAgentSelfUpdate Action = "agent.self-update"
	// ActionSwitchTransportMode instructs the agent to change its transport
	// mode between inbound (agent dials panel) and outbound (panel dials
	// agent). The job payload carries {"mode":"dial"|"listen","listen_addr":"..."}.
	ActionSwitchTransportMode Action = "switch_transport_mode"
)

// IsValidAction reports whether the action is a recognized job type.
func IsValidAction(a Action) bool {
	switch a {
	case ActionRuntimeReload,
		ActionUsersCreate,
		ActionClientCreate,
		ActionClientUpdate,
		ActionClientDelete,
		ActionClientRotateSecret,
		ActionClientResetQuota,
		ActionTelemetryRefreshDiagnostics,
		ActionAgentSelfUpdate,
		ActionSwitchTransportMode:
		return true
	default:
		return false
	}
}

// Status describes the orchestration lifecycle state of a job.
type Status string

const (
	// StatusQueued marks a job that is accepted and waiting for delivery.
	StatusQueued Status = "queued"
	// StatusRunning marks a job with at least one target already sent or acknowledged.
	StatusRunning Status = "running"
	// StatusSucceeded marks a job whose targets completed successfully.
	StatusSucceeded Status = "succeeded"
	// StatusFailed marks a job with at least one failed target.
	StatusFailed Status = "failed"
	// StatusExpired marks a job that exceeded its TTL before all targets completed.
	StatusExpired Status = "expired"
)

// TargetStatus describes the lifecycle state for one target inside a job.
type TargetStatus string

const (
	// TargetStatusQueued marks a target waiting for agent delivery.
	TargetStatusQueued TargetStatus = "queued"
	// TargetStatusSent marks a target command that was sent to an active agent stream.
	TargetStatusSent TargetStatus = "sent"
	// TargetStatusDelivered remains as a compatibility alias for historical code paths.
	TargetStatusDelivered TargetStatus = TargetStatusSent
	// TargetStatusAcknowledged marks a target command accepted by the agent runtime queue.
	TargetStatusAcknowledged TargetStatus = "acknowledged"
	// TargetStatusSucceeded marks a target that completed successfully.
	TargetStatusSucceeded TargetStatus = "succeeded"
	// TargetStatusFailed marks a target that completed with an execution error.
	TargetStatusFailed TargetStatus = "failed"
	// TargetStatusExpired marks a target that was never fully completed before TTL elapsed.
	TargetStatusExpired TargetStatus = "expired"
)

// JobTarget stores delivery and result state for one agent targeted by a job.
type JobTarget struct {
	AgentID    string       `json:"agent_id"`
	Status     TargetStatus `json:"status"`
	ResultText string       `json:"result_text"`
	ResultJSON string       `json:"result_json"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// Job stores the accepted job metadata required for later dispatch and auditing.
type Job struct {
	ID             string      `json:"id"`
	Action         Action      `json:"action"`
	TargetAgentIDs []string    `json:"target_agent_ids"`
	Targets        []JobTarget `json:"targets"`
	TTL            time.Duration `json:"ttl"`
	IdempotencyKey string      `json:"idempotency_key"`
	ActorID        string      `json:"actor_id"`
	Status         Status      `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	PayloadJSON    string      `json:"payload_json"`
}

// CreateJobInput contains the validation inputs required to enqueue a new job.
type CreateJobInput struct {
	Action         Action
	TargetAgentIDs []string
	TTL            time.Duration
	IdempotencyKey string
	ActorID        string
	ReadOnlyAgents map[string]bool
	PayloadJSON    string
}

// Service validates orchestration jobs before they enter the delivery queue.
//
// P-6: mu is an RWMutex so high-frequency read paths (QueueDepth on the
// metrics scrape loop, plus the read-mostly fast path of ListWithContext /
// PendingForAgent when no jobs are due to expire) do not serialize against
// each other or against the slow persistence-driving Enqueue path. Every
// mutating call site (Enqueue, *Locked helpers, prune workers, target
// updates) still takes the exclusive write lock.
type Service struct {
	mu         sync.RWMutex
	sequence   uint64
	updateSeq  uint64
	jobs       map[string]Job
	agentJobs  map[string]map[string]struct{}
	keys       map[string]string
	// keyTerminalAt records the time at which the job associated with the
	// idempotency key entered a terminal state (Succeeded, Failed, Expired).
	// Keys are removed from this map and from `keys` by PruneKeys once they
	// are older than the eviction TTL. Entries for jobs that are still
	// queued or running are NOT present; those keys stay live in `keys`
	// until the job completes.
	keyTerminalAt map[string]time.Time
	jobVersion map[string]uint64
	// latestSucceededByClient maps a Telemt client_id (extracted from a
	// client.* job's PayloadJSON) to the most recently observed succeeded
	// job for that client. Updated under s.mu whenever a client.* job
	// transitions into StatusSucceeded. Backs LatestSucceededWithContext
	// so the call site no longer needs an O(N) ListWithContext scan to
	// recover a client's connection_links result (P-4).
	latestSucceededByClient map[string]Job
	jobStore   storage.JobStore
	startupErr error
	now        func() time.Time
}

type persistCandidate struct {
	jobID   string
	version uint64
	job     Job
}

// NewService constructs an in-memory job validation and storage service.
func NewService() *Service {
	return &Service{
		jobs:                    make(map[string]Job),
		agentJobs:               make(map[string]map[string]struct{}),
		keys:                    make(map[string]string),
		keyTerminalAt:           make(map[string]time.Time),
		jobVersion:              make(map[string]uint64),
		latestSucceededByClient: make(map[string]Job),
		now:                     time.Now,
	}
}

// NewServiceWithStore constructs a job service that persists state through the shared store.
func NewServiceWithStore(jobStore storage.JobStore) *Service {
	service := &Service{
		jobs:                    make(map[string]Job),
		agentJobs:               make(map[string]map[string]struct{}),
		keys:                    make(map[string]string),
		keyTerminalAt:           make(map[string]time.Time),
		jobVersion:              make(map[string]uint64),
		latestSucceededByClient: make(map[string]Job),
		jobStore:                jobStore,
		now:                     time.Now,
	}
	service.startupErr = service.restore()
	return service
}

// isTerminalStatus reports whether the status represents a completed job
// whose idempotency key may be evicted after the configured TTL.
func isTerminalStatus(status Status) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusExpired:
		return true
	default:
		return false
	}
}

// markKeyTerminalLocked records that the job owning `key` entered a terminal
// state at `now`. Safe to call repeatedly; only the latest time is retained
// so retries of a once-terminal job still get evicted on the original TTL
// window. Caller must hold s.mu.
func (s *Service) markKeyTerminalLocked(key string, now time.Time) {
	if key == "" {
		return
	}
	if _, ok := s.keys[key]; !ok {
		return
	}
	s.keyTerminalAt[key] = now
}

// clearKeyTerminalLocked removes any terminal timestamp previously recorded
// for `key`. Used when a job transitions from a terminal state back to a
// live state (rare, but keeps the two maps consistent). Caller holds s.mu.
func (s *Service) clearKeyTerminalLocked(key string) {
	if key == "" {
		return
	}
	delete(s.keyTerminalAt, key)
}

// PruneKeys removes idempotency keys for jobs that reached a terminal state
// more than `olderThan` ago, along with the corresponding job and target
// entries. Returns the number of keys evicted. Safe to call concurrently.
//
// A TTL > 0 is required — calling with a non-positive TTL is a no-op so
// callers cannot accidentally delete live idempotency keys.
func (s *Service) PruneKeys(olderThan time.Duration) int {
	if olderThan <= 0 {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.now().Add(-olderThan)
	evicted := 0
	for key, terminalAt := range s.keyTerminalAt {
		if terminalAt.After(cutoff) {
			continue
		}
		jobID := s.keys[key]
		delete(s.keys, key)
		delete(s.keyTerminalAt, key)
		// Also drop the in-memory job record so both maps stay bounded.
		// The job has already been persisted in its terminal form; the
		// storage layer owns long-term retention for /api/audit-style
		// historical queries.
		if jobID != "" {
			// P-4: drop any latestSucceededByClient pointer that names
			// this jobID so the index never references an evicted job.
			if existingJob, ok := s.jobs[jobID]; ok {
				if cid := clientIDFromPayload(existingJob.Action, existingJob.PayloadJSON); cid != "" {
					if cur, has := s.latestSucceededByClient[cid]; has && cur.ID == jobID {
						delete(s.latestSucceededByClient, cid)
					}
				}
			}
			delete(s.jobs, jobID)
			delete(s.jobVersion, jobID)
		}
		evicted++
	}
	return evicted
}

// StartKeyEvictionWorker runs PruneKeys on a ticker until ctx is cancelled.
// `interval` controls how often the scan runs and `ttl` is the age beyond
// which terminal-state keys are evicted. The worker decrements wg on exit.
//
// Contract: wg.Add(1) is the caller's responsibility, mirroring the other
// background workers in the control-plane (rollup, update-checker). This
// lets the server.Close() path join every worker uniformly.
func (s *Service) StartKeyEvictionWorker(ctx context.Context, interval time.Duration, ttl time.Duration, wg *sync.WaitGroup) {
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.PruneKeys(ttl)
			}
		}
	}()
}

// StartupError reports the first restore error encountered while loading persisted job state.
func (s *Service) StartupError() error {
	return s.startupErr
}

// QueueDepth returns the number of jobs currently in the queued or running
// state. Exposed for metrics (panvex_job_queue_depth); counts only live jobs
// so terminal (succeeded/failed/expired) entries do not inflate the gauge.
//
// P-6: pure read; takes RLock so the metrics scraper does not block
// concurrent Enqueue / target-update writers.
func (s *Service) QueueDepth() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, job := range s.jobs {
		switch job.Status {
		case StatusQueued, StatusRunning:
			count++
		}
	}
	return count
}

// SetNow overrides the clock used for time-sensitive job checks.
func (s *Service) SetNow(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
}

// Enqueue validates the job input and records the queued job.
//
// P2-PERF-04: The synchronous DB persist (PutJob / PutJobTarget) is performed
// OUTSIDE the service mutex so concurrent readers such as PendingForAgent are
// not blocked by slow storage. The work is split into three phases:
//
//  1. Reserve under lock — validate input, reserve the idempotency key so
//     racing Enqueue calls with the same key observe the duplicate
//     immediately, and allocate a stable job ID.
//  2. Persist outside lock — run PutJob + PutJobTarget. Any other method that
//     only needs in-memory state continues to run without contention.
//  3. Finalize under lock — if persist succeeded, publish the job into the
//     s.jobs map / agentJobs index and bump the version counter. If persist
//     failed, roll back the reservation (remove the idempotency key) and
//     return the error to the caller.
//
// While persist is in flight the tentative job is NOT visible via
// PendingForAgent — it has not been added to s.jobs / s.agentJobs yet. Only
// the idempotency-key reservation in s.keys lives across the out-of-lock
// window, which is exactly what's needed to reject duplicate-key races.
func (s *Service) Enqueue(ctx context.Context, input CreateJobInput, now time.Time) (Job, error) {
	s.mu.Lock()

	if _, exists := s.keys[input.IdempotencyKey]; exists {
		s.mu.Unlock()
		return Job{}, ErrDuplicateIdempotencyKey
	}

	if isMutatingAction(input.Action) {
		for _, targetAgentID := range input.TargetAgentIDs {
			if input.ReadOnlyAgents[targetAgentID] {
				s.mu.Unlock()
				return Job{}, ErrReadOnlyTarget
			}
		}
	}

	s.sequence++
	jobID := fmt.Sprintf("job-%06d", s.sequence)
	job := Job{
		ID:             jobID,
		Action:         input.Action,
		TargetAgentIDs: append([]string(nil), input.TargetAgentIDs...),
		Targets:        make([]JobTarget, 0, len(input.TargetAgentIDs)),
		TTL:            input.TTL,
		IdempotencyKey: input.IdempotencyKey,
		ActorID:        input.ActorID,
		Status:         StatusQueued,
		CreatedAt:      now.UTC(),
		PayloadJSON:    input.PayloadJSON,
	}
	for _, agentID := range input.TargetAgentIDs {
		job.Targets = append(job.Targets, JobTarget{
			AgentID:   agentID,
			Status:    TargetStatusQueued,
			UpdatedAt: now.UTC(),
		})
	}

	// Reserve the idempotency key so racing Enqueues with the same key
	// observe the duplicate while we persist outside the lock. The empty
	// job ID marks the reservation as tentative; other code paths that
	// look up keys (e.g. PruneKeys) skip empty values.
	s.keys[input.IdempotencyKey] = ""
	s.mu.Unlock()

	if s.jobStore != nil {
		if err := s.persistJob(ctx, job); err != nil {
			s.mu.Lock()
			// Only remove if no one else has claimed the slot. We hold the
			// exclusive right to this key because no other Enqueue could
			// have flipped the sentinel — duplicates were rejected above.
			if existing, ok := s.keys[input.IdempotencyKey]; ok && existing == "" {
				delete(s.keys, input.IdempotencyKey)
			}
			s.mu.Unlock()
			return Job{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[input.IdempotencyKey] = job.ID
	s.updateSeq++
	s.jobVersion[job.ID] = s.updateSeq

	slog.InfoContext(ctx, "job dispatched",
		"job_id", job.ID,
		"action", string(job.Action),
		"target_agent_ids", job.TargetAgentIDs,
		"actor_id", job.ActorID,
	)

	return job, nil
}

func isMutatingAction(action Action) bool {
	switch action {
	case ActionUsersCreate, ActionRuntimeReload, ActionClientCreate, ActionClientUpdate, ActionClientDelete, ActionClientRotateSecret:
		return true
	default:
		return false
	}
}

// Get returns a snapshot of the job identified by jobID, if any. O(1)
// against the in-memory index (P-4); supersedes the historical pattern of
// scanning ListWithContext for a single ID.
func (s *Service) Get(jobID string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return Job{}, false
	}
	return cloneJob(job), true
}

// LatestSucceededWithContext returns the most recently observed succeeded
// client.* job for the given Telemt client_id, or (nil, false) if no such
// job has been recorded yet. The lookup is O(1) — backed by the
// latestSucceededByClient index that this package updates whenever a
// client.* job transitions into StatusSucceeded (P-4).
//
// The ctx parameter is reserved for future asynchronous storage hydration
// (parity with ListWithContext / RecordResult); the in-memory path does
// not currently consult ctx.
func (s *Service) LatestSucceededWithContext(_ context.Context, clientID string) (*Job, bool) {
	if clientID == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.latestSucceededByClient[clientID]
	if !ok {
		return nil, false
	}
	cloned := cloneJob(job)
	return &cloned, true
}

// clientIDFromPayload returns the Telemt client_id embedded in a client.*
// job's PayloadJSON, or "" if the action is not a client action or the
// payload is malformed. Decoupling this here keeps the index-maintenance
// path inside the jobs package without leaking the controlplane/server
// payload type into this lower-level package.
func clientIDFromPayload(action Action, payloadJSON string) string {
	switch action {
	case ActionClientCreate, ActionClientUpdate, ActionClientDelete, ActionClientRotateSecret:
	default:
		return ""
	}
	if payloadJSON == "" {
		return ""
	}
	var probe struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &probe); err != nil {
		return ""
	}
	return probe.ClientID
}

// indexLatestSucceededLocked records `job` as the most recent succeeded job
// for its embedded client_id, if and only if it is a succeeded client.*
// job. Caller must hold s.mu (write).
func (s *Service) indexLatestSucceededLocked(job Job) {
	if job.Status != StatusSucceeded {
		return
	}
	clientID := clientIDFromPayload(job.Action, job.PayloadJSON)
	if clientID == "" {
		return
	}
	if existing, ok := s.latestSucceededByClient[clientID]; ok {
		// Only overwrite if the new job is at least as recent as the
		// existing one. This keeps the index monotone-by-CreatedAt even
		// if results arrive out-of-order across agents.
		if !existing.CreatedAt.Before(job.CreatedAt) {
			return
		}
	}
	s.latestSucceededByClient[clientID] = cloneJob(job)
}

// List returns a snapshot of the queued jobs known to the service.
//
// List intentionally does not take a context because some callers (notably
// http_control_room) live outside this remediation cluster. The internal
// persist path uses context.Background() — this is acceptable because
// expiry-driven persists are housekeeping that must run regardless of any
// individual request being cancelled. New callers that hold a request
// context should prefer ListWithContext.
func (s *Service) List() []Job {
	return s.ListWithContext(context.Background())
}

// ListWithContext is the ctx-aware variant of List. The ctx is forwarded to
// any expiry-driven persistence performed by this call.
//
// P-6: hot read path. We take RLock first and check whether any job is due
// to expire. If none is, the snapshot is built entirely under RLock so
// concurrent List/Pending callers do not serialize on each other. Only when
// expiry work is actually required do we drop the read lock and re-acquire
// the exclusive write lock — racing readers may briefly see the pre-expiry
// state, which is fine because expiry is itself best-effort housekeeping.
func (s *Service) ListWithContext(ctx context.Context) []Job {
	s.mu.RLock()
	now := s.now().UTC()
	if !s.anyJobNeedsExpiryLocked(now) {
		result := make([]Job, 0, len(s.jobs))
		for _, job := range s.jobs {
			result = append(result, cloneJob(job))
		}
		s.mu.RUnlock()
		sortJobsByCreatedAt(result)
		return result
	}
	s.mu.RUnlock()

	s.mu.Lock()
	now = s.now().UTC()
	candidates := s.expireJobsLocked(now)
	result := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, cloneJob(job))
	}
	s.mu.Unlock()

	sortJobsByCreatedAt(result)

	for _, candidate := range candidates {
		s.persistLatestJobVersion(ctx, candidate.jobID, candidate.version, candidate.job)
	}

	return result
}

// sortJobsByCreatedAt orders jobs by CreatedAt ascending, breaking ties on
// ID so the output is deterministic for callers (UI, tests).
func sortJobsByCreatedAt(jobs []Job) {
	sort.Slice(jobs, func(left, right int) bool {
		if jobs[left].CreatedAt.Equal(jobs[right].CreatedAt) {
			return jobs[left].ID < jobs[right].ID
		}
		return jobs[left].CreatedAt.Before(jobs[right].CreatedAt)
	})
}

// anyJobNeedsExpiryLocked reports whether at least one queued/running job has
// passed its TTL at `now`. Read-only; safe under either RLock or Lock. Used
// by the read-mostly fast path of ListWithContext / PendingForAgent so we
// can skip the exclusive-lock branch entirely when nothing is due to expire.
func (s *Service) anyJobNeedsExpiryLocked(now time.Time) bool {
	for _, job := range s.jobs {
		if jobShouldExpire(job, now) {
			return true
		}
	}
	return false
}

// DefaultListRecentLimit caps the HTTP /jobs response so a long-lived
// control plane cannot ship unbounded payloads to the UI (Q2.U-P-13).
const DefaultListRecentLimit = 200

// ListRecentWithContext returns the most recently created jobs, sorted
// newest-first, capped at limit. A non-positive limit falls back to
// DefaultListRecentLimit. Internally reuses ListWithContext so expiry
// bookkeeping still runs on every call.
func (s *Service) ListRecentWithContext(ctx context.Context, limit int) []Job {
	if limit <= 0 || limit > DefaultListRecentLimit*5 {
		limit = DefaultListRecentLimit
	}
	all := s.ListWithContext(ctx)
	// ListWithContext sorts ascending; reverse to newest-first then trim.
	reversed := make([]Job, 0, limit)
	for i := len(all) - 1; i >= 0 && len(reversed) < limit; i-- {
		reversed = append(reversed, all[i])
	}
	return reversed
}

// ExpireStale proactively expires jobs that exceeded their TTL.
func (s *Service) ExpireStale() {
	s.mu.Lock()
	candidates := s.expireJobsLocked(s.now().UTC())
	s.mu.Unlock()

	for _, candidate := range candidates {
		s.persistLatestJobVersion(context.Background(), candidate.jobID, candidate.version, candidate.job)
	}
}

// PendingForAgent returns queued and stale-sent jobs for one agent in creation order.
//
// P-6: hottest read path on the long-poll Connect handler. Same fast/slow
// pattern as ListWithContext — the no-expiry branch never escalates past
// RLock, so concurrent agents polling for jobs do not serialize on each
// other or on the metrics scraper.
func (s *Service) PendingForAgent(ctx context.Context, agentID string, retryAfter time.Duration) []Job {
	s.mu.RLock()
	now := s.now().UTC()
	if !s.anyJobNeedsExpiryLocked(now) {
		jobsForAgent := s.agentJobs[agentID]
		result := make([]Job, 0, len(jobsForAgent))
		for jobID := range jobsForAgent {
			job, ok := s.jobs[jobID]
			if !ok {
				continue
			}
			if jobIsPendingForAgent(job, agentID, now, retryAfter) {
				result = append(result, cloneJob(job))
			}
		}
		s.mu.RUnlock()
		sortJobsByCreatedAt(result)
		return result
	}
	s.mu.RUnlock()

	s.mu.Lock()
	now = s.now().UTC()
	candidates := s.expireJobsLocked(now)

	jobsForAgent := s.agentJobs[agentID]
	result := make([]Job, 0, len(jobsForAgent))
	for jobID := range jobsForAgent {
		job, ok := s.jobs[jobID]
		if !ok {
			continue
		}
		if jobIsPendingForAgent(job, agentID, now, retryAfter) {
			result = append(result, cloneJob(job))
		}
	}
	s.mu.Unlock()

	sortJobsByCreatedAt(result)

	for _, candidate := range candidates {
		s.persistLatestJobVersion(ctx, candidate.jobID, candidate.version, candidate.job)
	}

	return result
}

// jobIsPendingForAgent reports whether the agent's first matching
// target should be re-dispatched at `now`. Pulled out of the locked
// critical section so each branch reads as a single intent.
func jobIsPendingForAgent(job Job, agentID string, now time.Time, retryAfter time.Duration) bool {
	for _, target := range job.Targets {
		if target.AgentID != agentID {
			continue
		}
		return targetIsPending(target, now, retryAfter)
	}
	return false
}

// targetIsPending decides whether a target needs (re-)dispatch at
// `now`. Queued is always included; sent and acknowledged are included
// only after the retryAfter window has elapsed (P2-LOG-05/L-14: ack
// state can survive a CP+agent restart without a result, so we let the
// agent's idempotency cache deduplicate on re-dispatch).
func targetIsPending(target JobTarget, now time.Time, retryAfter time.Duration) bool {
	switch target.Status {
	case TargetStatusQueued:
		return true
	case TargetStatusSent, TargetStatusAcknowledged:
		return target.UpdatedAt.IsZero() || !now.Before(target.UpdatedAt.Add(retryAfter))
	}
	return false
}

// MarkDelivered records that one target command has been sent to an active agent stream.
func (s *Service) MarkDelivered(ctx context.Context, agentID, jobID string, observedAt time.Time) {
	s.updateTarget(ctx, agentID, jobID, observedAt, func(target *JobTarget) {
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired {
			return
		}
		if target.Status == TargetStatusAcknowledged {
			// P2-LOG-05: re-dispatched post-restore ack targets stay in the
			// acknowledged state (so the result handler sees the correct
			// history), but the UpdatedAt bump via updateTarget gates the
			// next retryAfter window in PendingForAgent.
			return
		}
		target.Status = TargetStatusSent
	})
}

// MarkAcknowledged records that one target command has been accepted by the agent runtime queue.
func (s *Service) MarkAcknowledged(ctx context.Context, agentID, jobID string, observedAt time.Time) {
	s.updateTarget(ctx, agentID, jobID, observedAt, func(target *JobTarget) {
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired {
			return
		}
		if target.Status != TargetStatusSent && target.Status != TargetStatusAcknowledged {
			return
		}
		target.Status = TargetStatusAcknowledged
	})
}

// PruneAcknowledgedTargets expires targets that have been in the
// acknowledged state for longer than `olderThan` without a final result.
// This is the P2-LOG-05 safety net: an agent that restarts between ack and
// result will lose its in-flight command, and after the agent's own
// idempotency cache window elapses (2h by default, see
// defaultCompletedJobRetention) replaying is unsafe. At that point we must
// mark the target expired so it stops being re-dispatched and the key
// eviction worker can clean it up.
//
// Returns the number of targets transitioned to expired. olderThan <= 0 is
// a no-op so callers cannot accidentally expire live acknowledgements.
func (s *Service) PruneAcknowledgedTargets(ctx context.Context, olderThan time.Duration) int {
	if olderThan <= 0 {
		return 0
	}

	var candidates []persistCandidate

	s.mu.Lock()
	now := s.now().UTC()
	cutoff := now.Add(-olderThan)
	expired := 0

	for jobID, job := range s.jobs {
		jobExpired := expireAcknowledgedTargets(&job, cutoff, now)
		if jobExpired == 0 {
			continue
		}
		expired += jobExpired
		candidates = s.commitPrunedJobLocked(jobID, job, now, candidates)
	}
	s.mu.Unlock()

	for _, candidate := range candidates {
		s.persistLatestJobVersion(ctx, candidate.jobID, candidate.version, candidate.job)
	}

	return expired
}

// expireAcknowledgedTargets walks job.Targets in-place and flips each
// acknowledged target whose last update predates `cutoff` to expired.
// Returns how many targets were transitioned so the caller can decide
// whether the job needs to be re-derived and persisted.
func expireAcknowledgedTargets(job *Job, cutoff, now time.Time) int {
	expired := 0
	for index := range job.Targets {
		target := &job.Targets[index]
		if target.Status != TargetStatusAcknowledged {
			continue
		}
		if target.UpdatedAt.IsZero() || target.UpdatedAt.After(cutoff) {
			continue
		}
		target.Status = TargetStatusExpired
		target.UpdatedAt = now
		expired++
	}
	return expired
}

// commitPrunedJobLocked refreshes the in-memory indexes for a job whose
// acknowledged targets were just expired and, if a job store is wired,
// queues a persist candidate for the post-unlock fan-out. Caller must
// hold s.mu.
func (s *Service) commitPrunedJobLocked(jobID string, job Job, now time.Time, candidates []persistCandidate) []persistCandidate {
	job.Status = deriveJobStatus(job.Targets)
	s.jobs[jobID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[job.IdempotencyKey] = jobID
	if isTerminalStatus(job.Status) {
		s.markKeyTerminalLocked(job.IdempotencyKey, now)
	}
	s.indexLatestSucceededLocked(job)
	if s.jobStore == nil {
		return candidates
	}
	s.updateSeq++
	s.jobVersion[jobID] = s.updateSeq
	return append(candidates, persistCandidate{
		jobID:   jobID,
		version: s.updateSeq,
		job:     cloneJob(job),
	})
}

// StartAcknowledgedExpiryWorker runs PruneAcknowledgedTargets on a ticker
// until ctx is cancelled. Matches the StartKeyEvictionWorker contract —
// the caller owns wg.Add(1), the worker Done()s on exit. See P2-LOG-05.
func (s *Service) StartAcknowledgedExpiryWorker(ctx context.Context, interval time.Duration, ttl time.Duration, wg *sync.WaitGroup) {
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Background context: this worker is intentionally
				// detached from any request lifetime so periodic
				// expiry must run regardless of which contexts have
				// since been cancelled. The worker shuts down on
				// ctx.Done() above.
				s.PruneAcknowledgedTargets(ctx, ttl)
			}
		}
	}()
}

// RecordResult records the final agent-side execution result for one target.
// Returns true if the job was present in memory and the result was applied,
// false if the job has already been evicted (idempotent safety net — the
// caller should log a warning but treat this as non-fatal, since the ack
// expiry worker or terminal-key eviction may have dropped the job before
// the result arrived).
func (s *Service) RecordResult(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) bool {
	applied := s.updateTarget(ctx, agentID, jobID, observedAt, func(target *JobTarget) {
		if target.Status == TargetStatusExpired {
			return
		}
		if success {
			target.Status = TargetStatusSucceeded
		} else {
			target.Status = TargetStatusFailed
		}
		target.ResultText = message
		target.ResultJSON = resultJSON
	})
	if applied {
		// Look up the job's action for the log record. We tolerate a miss
		// (job may have been evicted between updateTarget and this read);
		// in that case we drop the action field rather than fabricating.
		var action string
		if job, ok := s.Get(jobID); ok {
			action = string(job.Action)
		}
		if success {
			slog.InfoContext(ctx, "job completed",
				"job_id", jobID,
				"agent_id", agentID,
				"action", action,
			)
		} else {
			slog.WarnContext(ctx, "job failed",
				"job_id", jobID,
				"agent_id", agentID,
				"action", action,
				"error", message,
			)
		}
	}
	return applied
}

// expireJobAndCollectCandidatesLocked transitions every still-active
// target on `job` to "expired" and marks the job itself as expired,
// returning a persist candidate when the store is configured. Caller
// must hold s.mu.
func (s *Service) expireJobAndCollectCandidatesLocked(job Job, now time.Time) []persistCandidate {
	updated := false
	for index := range job.Targets {
		target := &job.Targets[index]
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired {
			continue
		}
		target.Status = TargetStatusExpired
		target.UpdatedAt = now
		updated = true
	}
	if !updated && job.Status == StatusExpired {
		return nil
	}
	job.Status = StatusExpired
	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[job.IdempotencyKey] = job.ID
	s.markKeyTerminalLocked(job.IdempotencyKey, now)

	if s.jobStore == nil {
		return nil
	}
	s.updateSeq++
	s.jobVersion[job.ID] = s.updateSeq
	return []persistCandidate{{
		jobID:   job.ID,
		version: s.updateSeq,
		job:     cloneJob(job),
	}}
}

// applyTargetMutationLocked applies `mutate` to the job target whose
// AgentID matches `agentID` and rolls up the job-level status. Caller
// must hold s.mu. Returns the persist candidates produced; an empty
// slice means no agent-target match.
func (s *Service) applyTargetMutationLocked(job Job, agentID string, observedAt, now time.Time, mutate func(target *JobTarget)) []persistCandidate {
	updated := false
	for index := range job.Targets {
		if job.Targets[index].AgentID != agentID {
			continue
		}
		job.Targets[index].UpdatedAt = observedAt.UTC()
		mutate(&job.Targets[index])
		updated = true
		break
	}
	if !updated {
		return nil
	}

	job.Status = deriveJobStatus(job.Targets)
	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[job.IdempotencyKey] = job.ID
	if isTerminalStatus(job.Status) {
		s.markKeyTerminalLocked(job.IdempotencyKey, now)
	} else {
		s.clearKeyTerminalLocked(job.IdempotencyKey)
	}
	// P-4: keep latestSucceededByClient in sync with the canonical job map.
	s.indexLatestSucceededLocked(job)

	if s.jobStore == nil {
		return nil
	}
	s.updateSeq++
	s.jobVersion[job.ID] = s.updateSeq
	return []persistCandidate{{
		jobID:   job.ID,
		version: s.updateSeq,
		job:     cloneJob(job),
	}}
}

func (s *Service) updateTarget(ctx context.Context, agentID, jobID string, observedAt time.Time, mutate func(target *JobTarget)) bool {
	s.mu.Lock()
	now := s.now().UTC()

	job, ok := s.jobs[jobID]
	if !ok {
		// P2-LOG-05: the job was evicted (terminal-key TTL or ack expiry
		// worker) before this update arrived. Signal the caller so it can
		// log a warn and move on — dropping silently here would hide real
		// bugs where the agent sends results for unknown jobs.
		s.mu.Unlock()
		return false
	}

	var candidates []persistCandidate
	if jobShouldExpire(job, now) {
		candidates = s.expireJobAndCollectCandidatesLocked(job, now)
	} else {
		candidates = s.applyTargetMutationLocked(job, agentID, observedAt, now, mutate)
	}
	s.mu.Unlock()

	for _, candidate := range candidates {
		s.persistLatestJobVersion(ctx, candidate.jobID, candidate.version, candidate.job)
	}
	return true
}

func (s *Service) expireJobsLocked(now time.Time) []persistCandidate {
	persisting := s.jobStore != nil
	var candidates []persistCandidate
	if persisting {
		candidates = make([]persistCandidate, 0)
	}
	for _, job := range s.jobs {
		if !jobShouldExpire(job, now) {
			continue
		}
		updated, expiredJob, ok := expireJobTargets(job, now)
		if !ok {
			continue
		}
		s.applyExpiredJobLocked(expiredJob, now)
		if persisting && updated {
			s.updateSeq++
			s.jobVersion[expiredJob.ID] = s.updateSeq
			candidates = append(candidates, persistCandidate{
				jobID:   expiredJob.ID,
				version: s.updateSeq,
				job:     cloneJob(expiredJob),
			})
		}
	}
	return candidates
}

// expireJobTargets flips any non-terminal targets on the job to expired and
// returns the updated job. Reports updated=true when at least one target was
// transitioned. Returns ok=false when the job is already in StatusExpired with
// nothing to update — the caller should skip it.
func expireJobTargets(job Job, now time.Time) (bool, Job, bool) {
	updated := false
	for index := range job.Targets {
		target := &job.Targets[index]
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired {
			continue
		}
		target.Status = TargetStatusExpired
		target.UpdatedAt = now.UTC()
		updated = true
	}
	if !updated && job.Status == StatusExpired {
		return false, job, false
	}
	job.Status = StatusExpired
	return updated, job, true
}

// applyExpiredJobLocked commits the expired job back into the in-memory
// state and refreshes the per-key terminal-time index.
func (s *Service) applyExpiredJobLocked(job Job, now time.Time) {
	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[job.IdempotencyKey] = job.ID
	s.markKeyTerminalLocked(job.IdempotencyKey, now)
}

func jobShouldExpire(job Job, now time.Time) bool {
	if job.TTL <= 0 {
		return false
	}
	if job.Status != StatusQueued && job.Status != StatusRunning {
		return false
	}

	return now.After(job.CreatedAt.Add(job.TTL))
}

func (s *Service) restore() error {
	ctx := context.Background()
	// Q2.U-P-02: ListJobs returns the un-pruned tail of the jobs table.
	// PruneTerminalJobs (retention worker) keeps that tail bounded at
	// intervals.JobsSeconds, so in steady state the restore loop sees
	// only jobs from the last retention window. The in-flight redelivery
	// path requires every queued/running/acknowledged record, and the
	// admin UI requires recent terminal records for the activity feed —
	// hence the lack of a status filter here.
	jobsFromStore, err := s.jobStore.ListJobs(ctx)
	if err != nil {
		return err
	}

	// M-6: a single bulk fetch + map fan-out replaces the previous N+1
	// pattern (one ListJobTargets per job). On a large fleet with
	// thousands of restored jobs the per-call DB latency dominated
	// startup; one ORDER BY job_id, agent_id query keeps the same
	// per-job ordering downstream.
	allTargets, err := s.jobStore.ListAllJobTargets(ctx)
	if err != nil {
		return err
	}
	targetsByJob := make(map[string][]storage.JobTargetRecord, len(jobsFromStore))
	for _, target := range allTargets {
		targetsByJob[target.JobID] = append(targetsByJob[target.JobID], target)
	}

	for _, record := range jobsFromStore {
		// Q2.U-P-02: bounded restore is enforced by PruneTerminalJobs in
		// the retention worker — by the time we reach this loop the DB
		// only contains rows within JobsSeconds of "now", so loading
		// them all is safe. Skipping terminal jobs here would hide
		// recent failure history from the UI.
		s.installRestoredJob(buildJobWithTargets(record, targetsByJob[record.ID]))
	}

	return nil
}

// buildJobWithTargets composes a Job from a stored JobRecord and its
// already-fetched target rows. Same shape as the previous
// loadJobWithTargets helper, minus the per-job DB round-trip.
func buildJobWithTargets(record storage.JobRecord, targetRecords []storage.JobTargetRecord) Job {
	job := jobFromRecord(record)
	job.Targets = make([]JobTarget, 0, len(targetRecords))
	job.TargetAgentIDs = make([]string, 0, len(targetRecords))
	for _, targetRecord := range targetRecords {
		target := jobTargetFromRecord(targetRecord)
		job.Targets = append(job.Targets, target)
		job.TargetAgentIDs = append(job.TargetAgentIDs, target.AgentID)
	}
	return job
}

// installRestoredJob commits a single restored Job into the in-memory
// service state, including the agentJobs redelivery index, key bookkeeping,
// and update-sequence accounting.
func (s *Service) installRestoredJob(job Job) {
	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.reindexAcknowledgedTargets(job)
	s.keys[job.IdempotencyKey] = job.ID
	if isTerminalStatus(job.Status) {
		// Record the terminal timestamp so the eviction worker can
		// drop already-completed jobs that were restored from store.
		// We cannot recover the exact completion time cheaply, so use
		// the job's CreatedAt — older than cutoff means older TTL, and
		// the worst case is eviction on next scan, which is the
		// desired behaviour for jobs persisted long enough ago.
		s.keyTerminalAt[job.IdempotencyKey] = job.CreatedAt
	}
	// P-4: rebuild the per-client succeeded-job index from the restored
	// state so LatestSucceededWithContext returns the correct entry after
	// a control-plane restart.
	s.indexLatestSucceededLocked(job)
	s.sequence = maxJobSequence(s.sequence, job.ID)
	s.updateSeq++
	s.jobVersion[job.ID] = s.updateSeq
}

// reindexAcknowledgedTargets rebuilds the agentJobs entries for any target
// in TargetStatusAcknowledged. P2-LOG-05 (L-14): at runtime, MarkAcknowledged
// removes the target from agentJobs so PendingForAgent does not re-dispatch
// while the agent still owns the command. On restore, however, we must treat
// acknowledged-with-no-result as redeliverable: if both CP and agent
// restarted between ack and result, the agent's runtime queue is empty and
// the job would otherwise be stuck forever.
func (s *Service) reindexAcknowledgedTargets(job Job) {
	for _, target := range job.Targets {
		if target.AgentID == "" || target.Status != TargetStatusAcknowledged {
			continue
		}
		if s.agentJobs[target.AgentID] == nil {
			s.agentJobs[target.AgentID] = make(map[string]struct{})
		}
		s.agentJobs[target.AgentID][job.ID] = struct{}{}
	}
}

func (s *Service) persistJob(ctx context.Context, job Job) error {
	if err := s.jobStore.PutJob(ctx, jobToRecord(job)); err != nil {
		return err
	}
	for _, target := range job.Targets {
		if err := s.jobStore.PutJobTarget(ctx, jobTargetToRecord(job.ID, target)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) persistLatestJobVersion(ctx context.Context, jobID string, persistedVersion uint64, persistedJob Job) {
	for {
		if err := s.persistJob(ctx, persistedJob); err != nil {
			// Surface the persistence failure so a wedged DB does not
			// silently leave the in-memory job version ahead of the
			// store. The retry loop exits — the next mutation on this
			// job will trigger a fresh persist attempt; meanwhile the
			// operator notices via slog and (eventually) audit alerts.
			slog.Error("jobs: persist latest job version failed",
				"job_id", jobID,
				"persisted_version", persistedVersion,
				"error", err)
			return
		}

		s.mu.Lock()
		currentVersion, ok := s.jobVersion[jobID]
		if !ok || currentVersion == persistedVersion {
			s.mu.Unlock()
			return
		}
		latest, ok := s.jobs[jobID]
		if !ok {
			s.mu.Unlock()
			return
		}
		persistedVersion = currentVersion
		persistedJob = cloneJob(latest)
		s.mu.Unlock()
	}
}

func cloneJob(job Job) Job {
	cloned := job
	cloned.TargetAgentIDs = append([]string(nil), job.TargetAgentIDs...)
	cloned.Targets = append([]JobTarget(nil), job.Targets...)
	return cloned
}

func (s *Service) syncJobTargetsIndexLocked(job Job) {
	for _, target := range job.Targets {
		if target.AgentID == "" {
			continue
		}
		// Keep queued/sent AND acknowledged targets in the per-agent index.
		// Acknowledged must stay indexed so PendingForAgent can re-dispatch
		// it after the retryAfter window when the JobResult was lost between
		// ack and result (backpressure / stream drop / agent crash). Without
		// this the only recovery was a CP restart (reindexAcknowledgedTargets)
		// or TTL expiry. targetIsPending gates the actual re-dispatch by
		// retryAfter, and the agent's idempotency cache dedups the replay, so
		// retaining acknowledged here does not cause a dispatch storm. Only
		// terminal states (succeeded/failed/expired) drop out of the index.
		if target.Status == TargetStatusQueued || target.Status == TargetStatusSent || target.Status == TargetStatusAcknowledged {
			if s.agentJobs[target.AgentID] == nil {
				s.agentJobs[target.AgentID] = make(map[string]struct{})
			}
			s.agentJobs[target.AgentID][job.ID] = struct{}{}
			continue
		}

		agentJobs := s.agentJobs[target.AgentID]
		if agentJobs == nil {
			continue
		}
		delete(agentJobs, job.ID)
		if len(agentJobs) == 0 {
			delete(s.agentJobs, target.AgentID)
		}
	}
}

func deriveJobStatus(targets []JobTarget) Status {
	allSucceeded := len(targets) > 0
	anyProgress := false
	anyExpired := false
	for _, target := range targets {
		switch target.Status {
		case TargetStatusFailed:
			return StatusFailed
		case TargetStatusSucceeded:
			anyProgress = true
		case TargetStatusSent, TargetStatusAcknowledged:
			anyProgress = true
			allSucceeded = false
		case TargetStatusExpired:
			anyExpired = true
			allSucceeded = false
		default:
			allSucceeded = false
		}
	}

	if allSucceeded {
		return StatusSucceeded
	}
	if anyExpired {
		return StatusExpired
	}
	if anyProgress {
		return StatusRunning
	}

	return StatusQueued
}

func jobToRecord(job Job) storage.JobRecord {
	return storage.JobRecord{
		ID:             job.ID,
		Action:         string(job.Action),
		ActorID:        job.ActorID,
		Status:         string(job.Status),
		CreatedAt:      job.CreatedAt.UTC(),
		TTL:            job.TTL,
		IdempotencyKey: job.IdempotencyKey,
		PayloadJSON:    job.PayloadJSON,
	}
}

func jobFromRecord(record storage.JobRecord) Job {
	return Job{
		ID:             record.ID,
		Action:         Action(record.Action),
		TTL:            record.TTL,
		IdempotencyKey: record.IdempotencyKey,
		ActorID:        record.ActorID,
		Status:         Status(record.Status),
		CreatedAt:      record.CreatedAt.UTC(),
		PayloadJSON:    record.PayloadJSON,
	}
}

// JobFromRecord exposes the JobRecord -> Job transcription so HTTP-layer
// cursor pagination (S25 T1) can render store rows in the same shape as the
// in-memory Service uses, without duplicating the field map.
func JobFromRecord(record storage.JobRecord) Job {
	return jobFromRecord(record)
}

func jobTargetToRecord(jobID string, target JobTarget) storage.JobTargetRecord {
	return storage.JobTargetRecord{
		JobID:      jobID,
		AgentID:    target.AgentID,
		Status:     string(target.Status),
		ResultText: target.ResultText,
		ResultJSON: target.ResultJSON,
		UpdatedAt:  target.UpdatedAt.UTC(),
	}
}

func jobTargetFromRecord(record storage.JobTargetRecord) JobTarget {
	return JobTarget{
		AgentID:    record.AgentID,
		Status:     normalizedTargetStatus(TargetStatus(record.Status)),
		ResultText: record.ResultText,
		ResultJSON: record.ResultJSON,
		UpdatedAt:  record.UpdatedAt.UTC(),
	}
}

func normalizedTargetStatus(status TargetStatus) TargetStatus {
	if status == "delivered" {
		return TargetStatusSent
	}

	return status
}

func maxJobSequence(current uint64, jobID string) uint64 {
	if !strings.HasPrefix(jobID, "job-") {
		return current
	}

	value, err := strconv.ParseUint(strings.TrimPrefix(jobID, "job-"), 10, 64)
	if err != nil {
		return current
	}
	if value > current {
		return value
	}

	return current
}
