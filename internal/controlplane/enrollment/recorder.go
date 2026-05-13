package enrollment

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
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

// Store is the persistence boundary for enrollment attempts. The real
// implementation wraps sqlc-generated code; tests use an in-memory mock.
type Store interface {
	CreateAttempt(ctx context.Context, a Attempt) error
	AppendEvent(ctx context.Context, attemptID string, ev Event) error
	AttachAgent(ctx context.Context, attemptID, agentID string) error
	Complete(ctx context.Context, attemptID string, finishedAt time.Time) (changed bool, err error)
	Fail(ctx context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) (changed bool, err error)
}

// Publisher publishes timeline events on the existing /events bus.
type Publisher interface {
	Publish(eventType string, payload any)
}

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
		r.pub.Publish("enrollment.event", map[string]any{
			"attempt_id": attemptID,
			"step":       step,
			"level":      level,
			"message":    message,
			"fields":     fields,
			"ts":         ev.Ts,
		})
	}
}

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
		r.pub.Publish("enrollment.completed", map[string]any{
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
		r.pub.Publish("enrollment.failed", map[string]any{
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

// Ingest appends agent-reported events to the timeline preserving timestamps.
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
