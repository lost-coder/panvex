package enrollment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
// defaultListAttemptsLimit bounds a filter that asks for no limit. The endpoint
// is a debugging surface, not a bulk export.
const defaultListAttemptsLimit = 100

// ListAttempts returns the most recent enrollment attempts matching the filter,
// newest first, with keyset pagination.
//
// The filter is applied in SQL. It used to be applied in Go over a prefetched
// prefix of the 1000 newest rows, which meant a filter that matched nothing in
// that prefix silently returned empty — a node that failed to enroll yesterday
// was simply invisible once a thousand attempts had happened since, and the
// endpoint gave no hint that it had stopped looking.
//
// Bad UUIDs in the filter resolve to "no match" rather than an error so a
// malformed query string cannot break the endpoint.
func (s *SQLStore) ListAttempts(ctx context.Context, f ListFilter) ([]AttemptDTO, error) {
	if s.db == nil {
		return nil, errors.New("enrollment sqlstore: nil db")
	}

	var (
		conditions []string
		args       []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(clause, len(args)))
	}

	if f.TokenID != nil {
		if _, err := uuid.Parse(*f.TokenID); err != nil {
			// A syntactically invalid id can never match a stored row.
			return []AttemptDTO{}, nil
		}
		add("token_id = $%d", *f.TokenID)
	}
	if f.AgentID != nil {
		if _, err := uuid.Parse(*f.AgentID); err != nil {
			return []AttemptDTO{}, nil
		}
		add("agent_id = $%d", *f.AgentID)
	}
	if f.Status != nil {
		add("status = $%d", string(*f.Status))
	}
	if f.Mode != nil {
		add("mode = $%d", string(*f.Mode))
	}
	if f.ErrorCode != nil {
		add("error_code = $%d", *f.ErrorCode)
	}
	if f.StartedAfter != nil {
		add("started_at >= $%d", *f.StartedAfter)
	}
	if f.StartedBefore != nil {
		add("started_at < $%d", *f.StartedBefore)
	}
	// Keyset pagination over the (started_at DESC, id DESC) order: everything
	// strictly older than the cursor row.
	if f.CursorTs != nil {
		if f.CursorID != nil {
			args = append(args, *f.CursorTs, *f.CursorTs, *f.CursorID)
			conditions = append(conditions, fmt.Sprintf(
				"(started_at < $%d OR (started_at = $%d AND id < $%d))",
				len(args)-2, len(args)-1, len(args)))
		} else {
			add("started_at < $%d", *f.CursorTs)
		}
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultListAttemptsLimit
	}
	args = append(args, limit)

	query := `SELECT id, token_id, agent_id, mode, client_addr, request_id,
       status, error_code, error_message, started_at, finished_at
FROM enrollment_attempts`
	// The only thing concatenated into the SQL is placeholder text ($1, $2, ...)
	// built from the argument count above. Every filter VALUE travels as a bound
	// parameter; nothing derived from a request reaches the statement text.
	if len(conditions) > 0 {
		query += "\nWHERE " + strings.Join(conditions, " AND ") //nolint:gosec // reason: G202 — concatenated fragments are placeholder text only; all values are bound parameters.
	}
	query += fmt.Sprintf("\nORDER BY started_at DESC, id DESC\nLIMIT $%d", len(args)) //nolint:gosec // reason: G202 — the only interpolation is the placeholder index.

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AttemptDTO, 0, limit)
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
		out = append(out, rowToAttemptDTO(r))
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
		if err := json.Unmarshal(*r.FieldsJson, &f); err != nil {
			// A row we wrote ourselves failed to decode: the column is corrupt.
			// Surface it rather than returning a silently field-less event —
			// the operator would otherwise see a timeline entry whose detail
			// simply vanished (R8-D).
			slog.Warn("enrollment event fields are not decodable JSON; serving the event without them",
				"step", r.Step, "error", err)
		} else {
			dto.Fields = f
		}
	}
	return dto
}
