package jobs

import (
	"errors"
	"fmt"
	"sync"
	"time"
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
)

// Status describes the orchestration lifecycle state of a job.
type Status string

const (
	// StatusQueued marks a job that is accepted and waiting for delivery.
	StatusQueued Status = "queued"
)

// Job stores the accepted job metadata required for later dispatch and auditing.
type Job struct {
	ID             string
	Action         Action
	TargetAgentIDs []string
	TTL            time.Duration
	IdempotencyKey string
	ActorID        string
	Status         Status
	CreatedAt      time.Time
}

// CreateJobInput contains the validation inputs required to enqueue a new job.
type CreateJobInput struct {
	Action         Action
	TargetAgentIDs []string
	TTL            time.Duration
	IdempotencyKey string
	ActorID        string
	ReadOnlyAgents map[string]bool
}

// Service validates orchestration jobs before they enter the delivery queue.
type Service struct {
	mu       sync.Mutex
	sequence uint64
	jobs      map[string]Job
	keys      map[string]string
}

// NewService constructs an in-memory job validation and storage service.
func NewService() *Service {
	return &Service{
		jobs: make(map[string]Job),
		keys: make(map[string]string),
	}
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
		TTL:            input.TTL,
		IdempotencyKey: input.IdempotencyKey,
		ActorID:        input.ActorID,
		Status:         StatusQueued,
		CreatedAt:      now.UTC(),
	}

	s.jobs[job.ID] = job
	s.keys[input.IdempotencyKey] = job.ID

	return job, nil
}

func isMutatingAction(action Action) bool {
	switch action {
	case ActionUsersCreate, ActionRuntimeReload:
		return true
	default:
		return false
	}
}

// List returns a snapshot of the queued jobs known to the service.
func (s *Service) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job)
	}

	return result
}
