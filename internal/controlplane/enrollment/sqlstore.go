package enrollment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// SQLStore implements Store on top of sqlc-generated queries against
// either SQLite or Postgres (sqlc emits Postgres-style code that the
// project runs against both backends via database/sql).
type SQLStore struct {
	q *dbsqlc.Queries
}

func NewSQLStore(q *dbsqlc.Queries) *SQLStore {
	return &SQLStore{q: q}
}

func parseUUIDOrNull(s string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{}, nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return uuid.NullUUID{UUID: u, Valid: true}, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// CreateAttempt persists a new in_progress attempt row.
func (s *SQLStore) CreateAttempt(ctx context.Context, a Attempt) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("parse attempt id: %w", err)
	}
	tokenID, err := parseUUIDOrNull(a.TokenID)
	if err != nil {
		return err
	}
	agentID, err := parseUUIDOrNull(a.AgentID)
	if err != nil {
		return err
	}
	return s.q.CreateEnrollmentAttempt(ctx, dbsqlc.CreateEnrollmentAttemptParams{
		ID:         id,
		TokenID:    tokenID,
		AgentID:    agentID,
		Mode:       string(a.Mode),
		ClientAddr: nullString(a.ClientAddr),
		RequestID:  a.RequestID,
		StartedAt:  a.StartedAt,
	})
}

// AppendEvent persists one timeline event.
func (s *SQLStore) AppendEvent(ctx context.Context, attemptID string, ev Event) error {
	id, err := uuid.Parse(attemptID)
	if err != nil {
		return fmt.Errorf("parse attempt id: %w", err)
	}
	var fields json.RawMessage
	if ev.FieldsJSON != "" {
		fields = json.RawMessage(ev.FieldsJSON)
	}
	return s.q.AppendEnrollmentEvent(ctx, dbsqlc.AppendEnrollmentEventParams{
		AttemptID:  id,
		Ts:         ev.Ts,
		Step:       string(ev.Step),
		Level:      string(ev.Level),
		Message:    nullString(ev.Message),
		FieldsJson: fields,
	})
}

// AttachAgent sets the attempt's agent_id.
func (s *SQLStore) AttachAgent(ctx context.Context, attemptID, agentID string) error {
	id, err := uuid.Parse(attemptID)
	if err != nil {
		return fmt.Errorf("parse attempt id: %w", err)
	}
	aid, err := parseUUIDOrNull(agentID)
	if err != nil {
		return err
	}
	return s.q.AttachEnrollmentAttemptAgent(ctx, dbsqlc.AttachEnrollmentAttemptAgentParams{
		ID:      id,
		AgentID: aid,
	})
}

// Complete transitions an in_progress attempt to success.
// Returns (changed=false, nil) when the attempt is already terminal.
func (s *SQLStore) Complete(ctx context.Context, attemptID string, finishedAt time.Time) (bool, error) {
	id, err := uuid.Parse(attemptID)
	if err != nil {
		return false, fmt.Errorf("parse attempt id: %w", err)
	}
	rows, err := s.q.CompleteEnrollmentAttempt(ctx, dbsqlc.CompleteEnrollmentAttemptParams{
		ID:         id,
		FinishedAt: nullTime(finishedAt),
	})
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// Fail transitions an in_progress attempt to failed.
// Returns (changed=false, nil) when the attempt is already terminal.
func (s *SQLStore) Fail(ctx context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) (bool, error) {
	id, err := uuid.Parse(attemptID)
	if err != nil {
		return false, fmt.Errorf("parse attempt id: %w", err)
	}
	rows, err := s.q.FailEnrollmentAttempt(ctx, dbsqlc.FailEnrollmentAttemptParams{
		ID:           id,
		FinishedAt:   nullTime(finishedAt),
		ErrorCode:    nullString(string(code)),
		ErrorMessage: nullString(msg),
	})
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}
