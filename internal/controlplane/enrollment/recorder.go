package enrollment

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/events"
)

// Attempt is the persistent shape of an enrollment attempt.
type Attempt struct {
	ID         string
	TokenID    string
	AgentID    string
	Mode       Mode
	ClientAddr string
	RequestID  string
	Status     Status
	ErrorCode  ErrorCode
	ErrorMsg   string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Event is the persistent shape of one timeline event.
type Event struct {
	Step       Step
	Level      Level
	Message    string
	FieldsJSON string
	Ts         time.Time
}

// ListFilter constrains ListAttempts. Pointer fields are absent ⇒ no
// filter on that column. Limit ≤ 0 means "use the store's default".
type ListFilter struct {
	TokenID       *string
	AgentID       *string
	Status        *Status
	Mode          *Mode
	ErrorCode     *string
	StartedAfter  *time.Time
	StartedBefore *time.Time
	CursorTs      *time.Time
	CursorID      *string
	Limit         int
}

// AttemptPage carries one page of attempts plus an optional cursor
// pointing at the next page. NextCursor is nil at the end of results.
type AttemptPage struct {
	Items      []AttemptDTO
	NextCursor *AttemptCursor
}

// AttemptCursor identifies the boundary between two pages by the
// (started_at, id) pair of the last row of the previous page. Newer
// rows sort first; the next page starts with the first row strictly
// older than the cursor.
type AttemptCursor struct {
	Ts time.Time
	ID string
}

// AttemptDTO is the JSON-friendly projection of an Attempt returned by
// the read-side handlers. Empty optional columns are omitted via the
// `omitempty` tag so callers can distinguish "not set" from zero values.
type AttemptDTO struct {
	ID           string     `json:"id"`
	TokenID      string     `json:"token_id,omitempty"`
	AgentID      string     `json:"agent_id,omitempty"`
	Mode         Mode       `json:"mode"`
	ClientAddr   string     `json:"client_addr,omitempty"`
	RequestID    string     `json:"request_id"`
	Status       Status     `json:"status"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// EventDTO is the JSON-friendly projection of a stored timeline event.
type EventDTO struct {
	Step    Step           `json:"step"`
	Level   Level          `json:"level"`
	Message string         `json:"message,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
	Ts      time.Time      `json:"ts"`
}

// AttemptWithEvents bundles an attempt with its full ordered timeline.
type AttemptWithEvents struct {
	Attempt AttemptDTO `json:"attempt"`
	Events  []EventDTO `json:"events"`
}

// Store is the persistence boundary for enrollment attempts. The real
// implementation wraps sqlc-generated code; tests use an in-memory mock.
type Store interface {
	CreateAttempt(ctx context.Context, a Attempt) error
	AppendEvent(ctx context.Context, attemptID string, ev Event) error
	AttachAgent(ctx context.Context, attemptID, agentID string) error
	Complete(ctx context.Context, attemptID string, finishedAt time.Time) (changed bool, err error)
	Fail(ctx context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) (changed bool, err error)
	ListAttempts(ctx context.Context, f ListFilter) ([]AttemptDTO, error)
	GetWithEvents(ctx context.Context, id string) (*AttemptWithEvents, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// Publisher publishes timeline events on the existing /events bus.
type Publisher interface {
	Publish(eventType string, payload any)
}

// Recorder records the per-attempt timeline of enrollment.
//
// A single Recorder is safe for concurrent use by multiple goroutines once
// constructed: its fields are set only by NewRecorder and the WithPublisher /
// WithLogger copy-returning options.
//
// Persistence failures inside Event are logged via slog and silently dropped
// — timeline observability must never abort an in-flight enrollment. Ingest
// is the exception: it returns an error so the caller (the agent-side
// ReportEnrollmentSteps handler) can decide whether to retry.
type Recorder struct {
	store Store
	now   func() time.Time
	pub   Publisher
	log   *slog.Logger
}

func NewRecorder(store Store, now func() time.Time) *Recorder {
	return &Recorder{store: store, now: now, log: slog.Default()}
}

func (r *Recorder) WithPublisher(p Publisher) *Recorder {
	cp := *r
	cp.pub = p
	return &cp
}

func (r *Recorder) WithLogger(lg *slog.Logger) *Recorder {
	cp := *r
	cp.log = lg
	return &cp
}

func (r *Recorder) Begin(ctx context.Context, mode Mode, tokenID, clientAddr string) (string, error) {
	id := uuid.NewString()
	att := Attempt{
		ID:         id,
		TokenID:    tokenID,
		Mode:       mode,
		ClientAddr: clientAddr,
		RequestID:  RequestIDFromContext(ctx),
		Status:     StatusInProgress,
		StartedAt:  r.now().UTC(),
	}
	if err := r.store.CreateAttempt(ctx, att); err != nil {
		return "", err
	}
	r.log.LogAttrs(ctx, slog.LevelInfo, "enrollment attempt started",
		slog.String("attempt_id", id),
		slog.String("mode", string(mode)),
		slog.String("token_id", tokenID),
		slog.String("client_addr", clientAddr),
		slog.String("request_id", att.RequestID),
	)
	return id, nil
}

// Event records one stage in the attempt and dual-writes to slog. It never
// returns an error: a missed timeline write must not block enrollment.
// Storage failures surface only via the slog "enrollment event persist
// failed" line.
func (r *Recorder) Event(ctx context.Context, attemptID string, step Step, level Level, message string, fields map[string]any) {
	fieldsJSON := ""
	if len(fields) > 0 {
		if b, err := json.Marshal(fields); err == nil {
			fieldsJSON = string(b)
		}
	}
	ev := Event{
		Step:       step,
		Level:      level,
		Message:    message,
		FieldsJSON: fieldsJSON,
		Ts:         r.now().UTC(),
	}
	if err := r.store.AppendEvent(ctx, attemptID, ev); err != nil {
		r.log.LogAttrs(ctx, slog.LevelError, "enrollment event persist failed",
			slog.String("attempt_id", attemptID),
			slog.String("step", string(step)),
			slog.Any("error", err),
		)
	}
	attrs := []slog.Attr{
		slog.String("attempt_id", attemptID),
		slog.String("step", string(step)),
	}
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	r.log.LogAttrs(ctx, slogLevel(level), message, attrs...)

	if r.pub != nil {
		r.pub.Publish(events.TypeEnrollmentEvent, map[string]any{
			"attempt_id": attemptID,
			"step":       step,
			"level":      level,
			"message":    message,
			"fields":     fields,
			"ts":         ev.Ts,
		})
	}
}

// AttachAgent sets the attempt's agent_id once the panel has issued the
// cert and minted the agent row. Unlike Begin and Complete, it does not
// emit a slog line on its own — the surrounding handler logs are enough.
func (r *Recorder) AttachAgent(ctx context.Context, attemptID, agentID string) error {
	return r.store.AttachAgent(ctx, attemptID, agentID)
}

// Complete marks the attempt successful. No-op on already-terminal attempts.
func (r *Recorder) Complete(ctx context.Context, attemptID string) error {
	now := r.now().UTC()
	changed, err := r.store.Complete(ctx, attemptID, now)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	r.log.LogAttrs(ctx, slog.LevelInfo, "enrollment attempt succeeded",
		slog.String("attempt_id", attemptID),
	)
	if r.pub != nil {
		r.pub.Publish(events.TypeEnrollmentCompleted, map[string]any{
			"attempt_id": attemptID,
			"ts":         now,
		})
	}
	return nil
}

// Fail marks the attempt failed. No-op on already-terminal attempts.
func (r *Recorder) Fail(ctx context.Context, attemptID string, code ErrorCode, cause error, fields map[string]any) error {
	msg, ok := MessageFor(code)
	if !ok {
		msg = "Enrollment failed."
	}
	now := r.now().UTC()
	changed, err := r.store.Fail(ctx, attemptID, now, code, msg)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	attrs := []slog.Attr{
		slog.String("attempt_id", attemptID),
		slog.String("error_code", string(code)),
	}
	if cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	r.log.LogAttrs(ctx, slog.LevelError, "enrollment attempt failed", attrs...)

	if r.pub != nil {
		r.pub.Publish(events.TypeEnrollmentFailed, map[string]any{
			"attempt_id":    attemptID,
			"error_code":    code,
			"error_message": msg,
			"ts":            now,
		})
	}
	return nil
}

// AgentReportedEvent is one step recorded locally on the agent and shipped
// back to the panel after a successful first sync.
type AgentReportedEvent struct {
	Step    Step
	Level   Level
	Ts      time.Time
	Message string
	Fields  map[string]any
}

// Ingest appends agent-reported events to the timeline preserving original
// timestamps. The agent sends one batch per attempt after first_sync_ok,
// so partial-batch persistence on error is acceptable: the caller will not
// retry, and the panel-side events captured before this call still give a
// usable partial timeline. Phase 2 may wrap this in a store-level
// transaction if richer agent-retry semantics are needed.
func (r *Recorder) Ingest(ctx context.Context, attemptID string, events []AgentReportedEvent) error {
	for _, e := range events {
		fieldsJSON := ""
		if len(e.Fields) > 0 {
			if b, err := json.Marshal(e.Fields); err == nil {
				fieldsJSON = string(b)
			}
		}
		if err := r.store.AppendEvent(ctx, attemptID, Event{
			Step:       e.Step,
			Level:      e.Level,
			Message:    e.Message,
			FieldsJSON: fieldsJSON,
			Ts:         e.Ts.UTC(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ListAttempts returns recent attempts matching the filter, most-recent
// first. Used by the dashboard's enrollment-attempts list view.
func (r *Recorder) ListAttempts(ctx context.Context, f ListFilter) ([]AttemptDTO, error) {
	return r.store.ListAttempts(ctx, f)
}

// ListAttemptsPage fetches one page using filter + cursor and computes
// NextCursor. Limit defaults to 50 when non-positive. The store is
// asked for Limit+1 rows so the recorder can detect whether a next
// page exists without a second round-trip; the extra row is dropped
// from Items and its predecessor becomes the cursor.
func (r *Recorder) ListAttemptsPage(ctx context.Context, f ListFilter) (AttemptPage, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	pageLimit := f.Limit
	// Fetch one extra row to know if there's a next page.
	fetch := f
	fetch.Limit = pageLimit + 1
	items, err := r.store.ListAttempts(ctx, fetch)
	if err != nil {
		return AttemptPage{}, err
	}
	var next *AttemptCursor
	if len(items) > pageLimit {
		last := items[pageLimit-1]
		items = items[:pageLimit]
		next = &AttemptCursor{Ts: last.StartedAt, ID: last.ID}
	}
	return AttemptPage{Items: items, NextCursor: next}, nil
}

// GetAttemptWithEvents returns the attempt and its full ordered timeline
// for the detail view. Returns (nil, nil) when no attempt exists for id.
func (r *Recorder) GetAttemptWithEvents(ctx context.Context, id string) (*AttemptWithEvents, error) {
	return r.store.GetWithEvents(ctx, id)
}

// DeleteOlderThan removes attempts (and their cascaded events) whose
// started_at is strictly before cutoff. Returns the number of attempts
// removed for observability.
func (r *Recorder) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	return r.store.DeleteOlderThan(ctx, cutoff)
}

func slogLevel(l Level) slog.Level {
	switch l {
	case LevelError:
		return slog.LevelError
	case LevelWarn:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
