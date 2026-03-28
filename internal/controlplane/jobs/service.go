package jobs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
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
	// ActionTelemetryRefreshDiagnostics forces a fresh slow-diagnostics pull on the target node.
	ActionTelemetryRefreshDiagnostics Action = "telemetry.refresh_diagnostics"
)

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
type Service struct {
	mu         sync.Mutex
	sequence   uint64
	updateSeq  uint64
	jobs       map[string]Job
	agentJobs  map[string]map[string]struct{}
	keys       map[string]string
	jobVersion map[string]uint64
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
		jobs:       make(map[string]Job),
		agentJobs:  make(map[string]map[string]struct{}),
		keys:       make(map[string]string),
		jobVersion: make(map[string]uint64),
		now:        time.Now,
	}
}

// NewServiceWithStore constructs a job service that persists state through the shared store.
func NewServiceWithStore(jobStore storage.JobStore) *Service {
	service := &Service{
		jobs:       make(map[string]Job),
		agentJobs:  make(map[string]map[string]struct{}),
		keys:       make(map[string]string),
		jobVersion: make(map[string]uint64),
		jobStore:   jobStore,
		now:        time.Now,
	}
	service.startupErr = service.restore()
	return service
}

// StartupError reports the first restore error encountered while loading persisted job state.
func (s *Service) StartupError() error {
	return s.startupErr
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
func (s *Service) Enqueue(input CreateJobInput, now time.Time) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[input.IdempotencyKey]; exists {
		return Job{}, ErrDuplicateIdempotencyKey
	}

	if isMutatingAction(input.Action) {
		for _, targetAgentID := range input.TargetAgentIDs {
			if input.ReadOnlyAgents[targetAgentID] {
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

	if s.jobStore != nil {
		if err := s.persistJob(context.Background(), job); err != nil {
			return Job{}, err
		}
	}

	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[input.IdempotencyKey] = job.ID
	s.updateSeq++
	s.jobVersion[job.ID] = s.updateSeq

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

// List returns a snapshot of the queued jobs known to the service.
func (s *Service) List() []Job {
	var candidates []persistCandidate

	s.mu.Lock()
	now := s.now().UTC()
	candidates = s.expireJobsLocked(now)

	result := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, cloneJob(job))
	}
	s.mu.Unlock()

	sort.Slice(result, func(left int, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})

	for _, candidate := range candidates {
		s.persistLatestJobVersion(candidate.jobID, candidate.version, candidate.job)
	}

	return result
}

// PendingForAgent returns queued and stale-sent jobs for one agent in creation order.
func (s *Service) PendingForAgent(agentID string, retryAfter time.Duration) []Job {
	var candidates []persistCandidate

	s.mu.Lock()
	now := s.now().UTC()
	candidates = s.expireJobsLocked(now)

	jobsForAgent := s.agentJobs[agentID]
	result := make([]Job, 0, len(jobsForAgent))
	for jobID := range jobsForAgent {
		job, ok := s.jobs[jobID]
		if !ok {
			continue
		}
		for _, target := range job.Targets {
			if target.AgentID != agentID {
				continue
			}
			include := false
			switch target.Status {
			case TargetStatusQueued:
				include = true
			case TargetStatusSent:
				if target.UpdatedAt.IsZero() || !now.Before(target.UpdatedAt.Add(retryAfter)) {
					include = true
				}
			}
			if include {
				result = append(result, cloneJob(job))
			}
			break
		}
	}
	s.mu.Unlock()

	sort.Slice(result, func(left int, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})

	for _, candidate := range candidates {
		s.persistLatestJobVersion(candidate.jobID, candidate.version, candidate.job)
	}

	return result
}

// MarkDelivered records that one target command has been sent to an active agent stream.
func (s *Service) MarkDelivered(agentID string, jobID string, observedAt time.Time) {
	s.updateTarget(agentID, jobID, observedAt, func(target *JobTarget) {
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired || target.Status == TargetStatusAcknowledged {
			return
		}
		target.Status = TargetStatusSent
	})
}

// MarkAcknowledged records that one target command has been accepted by the agent runtime queue.
func (s *Service) MarkAcknowledged(agentID string, jobID string, observedAt time.Time) {
	s.updateTarget(agentID, jobID, observedAt, func(target *JobTarget) {
		if target.Status == TargetStatusSucceeded || target.Status == TargetStatusFailed || target.Status == TargetStatusExpired {
			return
		}
		if target.Status != TargetStatusSent && target.Status != TargetStatusAcknowledged {
			return
		}
		target.Status = TargetStatusAcknowledged
	})
}

// RecordResult records the final agent-side execution result for one target.
func (s *Service) RecordResult(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.updateTarget(agentID, jobID, observedAt, func(target *JobTarget) {
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
}

func (s *Service) updateTarget(agentID string, jobID string, observedAt time.Time, mutate func(target *JobTarget)) {
	var (
		candidates []persistCandidate
	)

	s.mu.Lock()
	now := s.now().UTC()

	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		for _, candidate := range candidates {
			s.persistLatestJobVersion(candidate.jobID, candidate.version, candidate.job)
		}
		return
	}

	if jobShouldExpire(job, now) {
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
		if updated || job.Status != StatusExpired {
			job.Status = StatusExpired
			s.jobs[job.ID] = job
			s.syncJobTargetsIndexLocked(job)
			s.keys[job.IdempotencyKey] = job.ID

			if s.jobStore != nil {
				s.updateSeq++
				s.jobVersion[job.ID] = s.updateSeq
				candidates = append(candidates, persistCandidate{
					jobID:   job.ID,
					version: s.updateSeq,
					job:     cloneJob(job),
				})
			}
		}
	}

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
		s.mu.Unlock()
		for _, candidate := range candidates {
			s.persistLatestJobVersion(candidate.jobID, candidate.version, candidate.job)
		}
		return
	}

	job.Status = deriveJobStatus(job.Targets)
	s.jobs[job.ID] = job
	s.syncJobTargetsIndexLocked(job)
	s.keys[job.IdempotencyKey] = job.ID

	if s.jobStore != nil {
		s.updateSeq++
		s.jobVersion[job.ID] = s.updateSeq
		candidates = append(candidates, persistCandidate{
			jobID:   job.ID,
			version: s.updateSeq,
			job:     cloneJob(job),
		})
	}
	s.mu.Unlock()

	for _, candidate := range candidates {
		s.persistLatestJobVersion(candidate.jobID, candidate.version, candidate.job)
	}
}

func (s *Service) expireJobsLocked(now time.Time) []persistCandidate {
	if s.jobStore == nil {
		for _, job := range s.jobs {
			if !jobShouldExpire(job, now) {
				continue
			}

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
				continue
			}

			job.Status = StatusExpired
			s.jobs[job.ID] = job
			s.syncJobTargetsIndexLocked(job)
			s.keys[job.IdempotencyKey] = job.ID
		}

		return nil
	}

	candidates := make([]persistCandidate, 0)
	for _, job := range s.jobs {
		if !jobShouldExpire(job, now) {
			continue
		}

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
			continue
		}

		job.Status = StatusExpired
		s.jobs[job.ID] = job
		s.syncJobTargetsIndexLocked(job)
		s.keys[job.IdempotencyKey] = job.ID
		s.updateSeq++
		s.jobVersion[job.ID] = s.updateSeq
		candidates = append(candidates, persistCandidate{
			jobID:   job.ID,
			version: s.updateSeq,
			job:     cloneJob(job),
		})
	}

	return candidates
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
	jobsFromStore, err := s.jobStore.ListJobs(context.Background())
	if err != nil {
		return err
	}

	for _, record := range jobsFromStore {
		targetRecords, err := s.jobStore.ListJobTargets(context.Background(), record.ID)
		if err != nil {
			return err
		}

		job := jobFromRecord(record)
		job.Targets = make([]JobTarget, 0, len(targetRecords))
		job.TargetAgentIDs = make([]string, 0, len(targetRecords))
		for _, targetRecord := range targetRecords {
			target := jobTargetFromRecord(targetRecord)
			job.Targets = append(job.Targets, target)
			job.TargetAgentIDs = append(job.TargetAgentIDs, target.AgentID)
		}

		s.jobs[job.ID] = job
		s.syncJobTargetsIndexLocked(job)
		s.keys[job.IdempotencyKey] = job.ID
		s.sequence = maxJobSequence(s.sequence, job.ID)
		s.updateSeq++
		s.jobVersion[job.ID] = s.updateSeq
	}

	return nil
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

func (s *Service) persistLatestJobVersion(jobID string, persistedVersion uint64, persistedJob Job) {
	for {
		if err := s.persistJob(context.Background(), persistedJob); err != nil {
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
		if target.Status == TargetStatusQueued || target.Status == TargetStatusSent {
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
