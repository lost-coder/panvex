package enrollment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// SQLStore implements Store on top of sqlc-generated queries against
// either SQLite or Postgres (sqlc emits Postgres-style code that the
// project runs against both backends via database/sql).
//
// One method — ListAttempts — bypasses the generated query and runs a
// hand-rolled SELECT directly against db. The reason is that the sqlc
// source uses sqlc.narg(...)::uuid / ::text casts to make the filters
// optional; those Postgres-style casts are accepted by the Postgres
// driver but rejected by modernc.org/sqlite at prepare time. Rather
// than ship two parallel sqlc dialects (and a second column-named
// query file), we filter in Go after pulling a bounded prefix from the
// SQL side. Phase 2 can revisit if attempt volume warrants pushing
// filters back into the DB.
type SQLStore struct {
	q  *dbsqlc.Queries
	db *sql.DB
}

// NewSQLStore wraps a sqlc Queries handle for the recorder. db is the
// underlying connection pool, used only by ListAttempts for the
// hand-rolled SELECT described on the type doc; the rest of the methods
// go through q. Passing a nil db is allowed for paths that never call
// ListAttempts (e.g. write-only fixtures), but the panel server always
// supplies both.
func NewSQLStore(q *dbsqlc.Queries, db *sql.DB) *SQLStore {
	return &SQLStore{q: q, db: db}
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
	var fields *json.RawMessage
	if ev.FieldsJSON != "" {
		raw := json.RawMessage(ev.FieldsJSON)
		fields = &raw
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

// listAttemptsPrefetchCap bounds the unfiltered prefix we read from the
// DB before in-Go filtering. The list endpoint caps the caller's limit
// at 100, and per the type doc filters are applied client-side; pulling
// a 1k prefix keeps the hot path cheap while leaving plenty of headroom
// for filtered queries that target older rows. Phase 2 can revisit if
// telemetry shows attempts accumulating past the prefix before
// retention prunes them.
const listAttemptsPrefetchCap = 1000

// ListAttempts pulls a bounded most-recent prefix from
// enrollment_attempts via a hand-rolled SELECT (see the SQLStore type
// doc for why we bypass sqlc here) and applies the ListFilter in Go.
// Bad UUIDs in the filter resolve to "no match" rather than an error so
// a malformed query string can't break the endpoint.
func (s *SQLStore) ListAttempts(ctx context.Context, f ListFilter) ([]AttemptDTO, error) {
	if s.db == nil {
		return nil, errors.New("enrollment sqlstore: nil db")
	}
	const q = `SELECT id, token_id, agent_id, mode, client_addr, request_id,
       status, error_code, error_message, started_at, finished_at
FROM enrollment_attempts
ORDER BY started_at DESC, id DESC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, listAttemptsPrefetchCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		wantToken     string
		wantTokenSet  bool
		wantAgent     string
		wantAgentSet  bool
		wantStatus    string
		wantStatusSet bool
		wantMode      string
		wantModeSet   bool
		wantErrCode   string
		wantErrSet    bool
		cursorIDStr   string
		cursorIDSet   bool
	)
	if f.TokenID != nil {
		if _, perr := uuid.Parse(*f.TokenID); perr == nil {
			wantToken = *f.TokenID
			wantTokenSet = true
		} else {
			// A syntactically invalid token_id can never match a stored
			// row, so short-circuit to an empty result rather than
			// scanning the prefix.
			return []AttemptDTO{}, nil
		}
	}
	if f.AgentID != nil {
		if _, perr := uuid.Parse(*f.AgentID); perr == nil {
			wantAgent = *f.AgentID
			wantAgentSet = true
		} else {
			return []AttemptDTO{}, nil
		}
	}
	if f.Status != nil {
		wantStatus = string(*f.Status)
		wantStatusSet = true
	}
	if f.Mode != nil {
		wantMode = string(*f.Mode)
		wantModeSet = true
	}
	if f.ErrorCode != nil {
		wantErrCode = *f.ErrorCode
		wantErrSet = true
	}
	if f.CursorID != nil {
		cursorIDStr = *f.CursorID
		cursorIDSet = true
	}

	out := make([]AttemptDTO, 0, f.Limit)
	for rows.Next() {
		var r dbsqlc.EnrollmentAttempt
		if err := rows.Scan(
			&r.ID,
			&r.TokenID,
			&r.AgentID,
			&r.Mode,
			&r.ClientAddr,
			&r.RequestID,
			&r.Status,
			&r.ErrorCode,
			&r.ErrorMessage,
			&r.StartedAt,
			&r.FinishedAt,
		); err != nil {
			return nil, err
		}
		if wantTokenSet {
			if !r.TokenID.Valid || r.TokenID.UUID.String() != wantToken {
				continue
			}
		}
		if wantAgentSet {
			if !r.AgentID.Valid || r.AgentID.UUID.String() != wantAgent {
				continue
			}
		}
		if wantStatusSet && r.Status != wantStatus {
			continue
		}
		if wantModeSet && r.Mode != wantMode {
			continue
		}
		if wantErrSet {
			if !r.ErrorCode.Valid || r.ErrorCode.String != wantErrCode {
				continue
			}
		}
		if f.StartedAfter != nil && r.StartedAt.Before(*f.StartedAfter) {
			continue
		}
		if f.StartedBefore != nil && !r.StartedAt.Before(*f.StartedBefore) {
			continue
		}
		if f.CursorTs != nil {
			if r.StartedAt.After(*f.CursorTs) {
				continue
			}
			if r.StartedAt.Equal(*f.CursorTs) {
				if !cursorIDSet || r.ID.String() >= cursorIDStr {
					continue
				}
			}
		}
		out = append(out, rowToAttemptDTO(r))
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWithEvents joins the attempt row and its timeline into a single
// response. Returns (nil, nil) for an unknown id so the handler can
// surface a 404 without inspecting the error string.
func (s *SQLStore) GetWithEvents(ctx context.Context, id string) (*AttemptWithEvents, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse id: %w", err)
	}
	row, err := s.q.GetEnrollmentAttempt(ctx, parsed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	eventRows, err := s.q.ListEnrollmentEvents(ctx, parsed)
	if err != nil {
		return nil, err
	}
	events := make([]EventDTO, 0, len(eventRows))
	for _, er := range eventRows {
		events = append(events, rowToEventDTO(er))
	}
	res := AttemptWithEvents{Attempt: rowToAttemptDTO(row), Events: events}
	return &res, nil
}

// DeleteOlderThan delegates to the sqlc :execrows query which returns
// the affected row count directly.
func (s *SQLStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.q.DeleteOldEnrollmentAttempts(ctx, cutoff)
}

func rowToAttemptDTO(r dbsqlc.EnrollmentAttempt) AttemptDTO {
	dto := AttemptDTO{
		ID:        r.ID.String(),
		Mode:      Mode(r.Mode),
		Status:    Status(r.Status),
		RequestID: r.RequestID,
		StartedAt: r.StartedAt,
	}
	if r.TokenID.Valid {
		dto.TokenID = r.TokenID.UUID.String()
	}
	if r.AgentID.Valid {
		dto.AgentID = r.AgentID.UUID.String()
	}
	if r.ClientAddr.Valid {
		dto.ClientAddr = r.ClientAddr.String
	}
	if r.ErrorCode.Valid {
		dto.ErrorCode = r.ErrorCode.String
	}
	if r.ErrorMessage.Valid {
		dto.ErrorMessage = r.ErrorMessage.String
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		dto.FinishedAt = &t
	}
	return dto
}

func rowToEventDTO(r dbsqlc.EnrollmentEvent) EventDTO {
	dto := EventDTO{Step: Step(r.Step), Level: Level(r.Level), Ts: r.Ts}
	if r.Message.Valid {
		dto.Message = r.Message.String
	}
	if r.FieldsJson != nil {
		var f map[string]any
		if err := json.Unmarshal(*r.FieldsJson, &f); err == nil {
			dto.Fields = f
		}
	}
	return dto
}
