package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// WebhookStore implements webhooks.Storage on top of pgx-backed
// *sql.DB. The Postgres dialect lets multiple worker replicas share
// the outbox safely via FOR UPDATE SKIP LOCKED on the claim path —
// a feature SQLite doesn't have, hence the separate file.
type WebhookStore struct {
	db      *sql.DB
	decrypt webhooks.SecretDecrypter
}

// NewWebhookStore wires a webhook storage backend over the given
// pool. decrypt mirrors sqlite.NewWebhookStore — see its godoc.
func NewWebhookStore(db *sql.DB, decrypt webhooks.SecretDecrypter) *WebhookStore {
	if decrypt == nil {
		decrypt = func(s string) ([]byte, error) { return []byte(s), nil }
	}
	return &WebhookStore{db: db, decrypt: decrypt}
}

// ListEnabledEndpoints returns every enabled webhook_endpoints row
// with the secret already decrypted.
func (s *WebhookStore) ListEnabledEndpoints(ctx context.Context) ([]webhooks.Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, url, secret_ciphertext, event_filter, allow_private, enabled
		FROM webhook_endpoints
		WHERE enabled = TRUE
	`)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list endpoints: %w", err)
	}
	defer rows.Close()
	var out []webhooks.Endpoint
	for rows.Next() {
		ep, err := scanWebhookEndpoint(rows, s.decrypt)
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: list endpoints rows: %w", err)
	}
	return out, nil
}

// InsertOutbox creates a pending delivery row.
func (s *WebhookStore) InsertOutbox(ctx context.Context, row webhooks.OutboxRow) error {
	payload := row.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_outbox
			(id, endpoint_id, event_action, payload, attempt, next_attempt_at, last_error, dead, created_at)
		VALUES
			($1, $2, $3, $4::jsonb, $5, $6, '', FALSE, $7)
	`,
		row.ID,
		row.EndpointID,
		row.EventAction,
		string(payload),
		row.Attempt,
		row.NextAttemptAt.UTC(),
		row.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("webhooks: insert outbox: %w", err)
	}
	return nil
}

// ClaimReady atomically reserves up to max ready rows in a single
// transaction using FOR UPDATE SKIP LOCKED, which lets multiple
// workers race without re-delivering. The transaction commits as
// soon as the rows are read; the worker performs the actual HTTP
// POST outside the transaction.
//
// SKIP LOCKED + ORDER BY may produce slightly out-of-order delivery
// when contention is high — acceptable for an at-least-once webhook
// queue (receivers must be idempotent on X-Panvex-Delivery anyway).
func (s *WebhookStore) ClaimReady(ctx context.Context, now time.Time, max int) ([]webhooks.Delivery, error) {
	if max <= 0 {
		max = 32
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("webhooks: begin claim tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on commit-success path
	rows, err := tx.QueryContext(ctx, `
		SELECT
			o.id, o.endpoint_id, o.event_action, o.payload::text,
			o.attempt, o.next_attempt_at, o.last_error, o.dead,
			o.created_at, o.delivered_at,
			e.id, e.name, e.url, e.secret_ciphertext, e.event_filter,
			e.allow_private, e.enabled
		FROM webhook_outbox o
		INNER JOIN webhook_endpoints e ON e.id = o.endpoint_id
		WHERE o.dead = FALSE
		  AND o.delivered_at IS NULL
		  AND o.next_attempt_at <= $1
		  AND e.enabled = TRUE
		ORDER BY o.next_attempt_at ASC, o.id ASC
		LIMIT $2
		FOR UPDATE OF o SKIP LOCKED
	`, now.UTC(), max)
	if err != nil {
		return nil, fmt.Errorf("webhooks: claim ready: %w", err)
	}
	defer rows.Close()

	var out []webhooks.Delivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows, s.decrypt)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: claim ready rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("webhooks: commit claim tx: %w", err)
	}
	return out, nil
}

func (s *WebhookStore) MarkDelivered(ctx context.Context, id string, deliveredAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhook_outbox
		SET delivered_at = $1, last_error = ''
		WHERE id = $2
	`, deliveredAt.UTC(), id)
	if err != nil {
		return fmt.Errorf("webhooks: mark delivered: %w", err)
	}
	return checkWebhookAffected(res)
}

func (s *WebhookStore) MarkFailed(ctx context.Context, id string, attempt int, nextAttempt time.Time, errMsg string, dead bool) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhook_outbox
		SET attempt = $1, next_attempt_at = $2, last_error = $3, dead = $4
		WHERE id = $5
	`, attempt, nextAttempt.UTC(), errMsg, dead, id)
	if err != nil {
		return fmt.Errorf("webhooks: mark failed: %w", err)
	}
	return checkWebhookAffected(res)
}

func scanWebhookEndpoint(s *sql.Rows, decrypt webhooks.SecretDecrypter) (webhooks.Endpoint, error) {
	var (
		ep           webhooks.Endpoint
		secretCipher string
		filterCSV    string
		allowPrivate bool
		enabled      bool
	)
	if err := s.Scan(&ep.ID, &ep.Name, &ep.URL, &secretCipher, &filterCSV, &allowPrivate, &enabled); err != nil {
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: scan endpoint: %w", err)
	}
	plain, err := decrypt(secretCipher)
	if err != nil {
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: decrypt endpoint %q secret: %w", ep.ID, err)
	}
	ep.Secret = plain
	ep.EventFilter = parseWebhookFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivate
	ep.Enabled = enabled
	return ep, nil
}

func scanWebhookDelivery(s *sql.Rows, decrypt webhooks.SecretDecrypter) (webhooks.Delivery, error) {
	var (
		row          webhooks.OutboxRow
		payloadStr   string
		nextAttempt  time.Time
		createdAt    time.Time
		deliveredAt  sql.NullTime
		dead         bool
		ep           webhooks.Endpoint
		secretCipher string
		filterCSV    string
		allowPrivate bool
		enabled      bool
	)
	if err := s.Scan(
		&row.ID, &row.EndpointID, &row.EventAction, &payloadStr,
		&row.Attempt, &nextAttempt, &row.LastError, &dead,
		&createdAt, &deliveredAt,
		&ep.ID, &ep.Name, &ep.URL, &secretCipher, &filterCSV,
		&allowPrivate, &enabled,
	); err != nil {
		return webhooks.Delivery{}, fmt.Errorf("webhooks: scan delivery: %w", err)
	}
	row.Payload = json.RawMessage(payloadStr)
	row.NextAttemptAt = nextAttempt.UTC()
	row.CreatedAt = createdAt.UTC()
	row.Dead = dead
	if deliveredAt.Valid {
		t := deliveredAt.Time.UTC()
		row.DeliveredAt = &t
	}
	plain, err := decrypt(secretCipher)
	if err != nil {
		return webhooks.Delivery{}, fmt.Errorf("webhooks: decrypt endpoint %q secret: %w", ep.ID, err)
	}
	ep.Secret = plain
	ep.EventFilter = parseWebhookFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivate
	ep.Enabled = enabled
	return webhooks.Delivery{Outbox: row, Endpoint: ep}, nil
}

func parseWebhookFilterCSV(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func checkWebhookAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhooks: rows affected: %w", err)
	}
	if n == 0 {
		return webhooks.ErrNotFound
	}
	return nil
}

// CRUD — Postgres mirror of sqlite/webhooks.go. Same secret-elision
// rules: meta reads do NOT return SecretCiphertext.

func (s *WebhookStore) CreateEndpoint(ctx context.Context, in webhooks.EndpointInput, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_endpoints
			(id, name, url, secret_ciphertext, event_filter, allow_private, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, in.ID, in.Name, in.URL, in.SecretCiphertext, in.EventFilter, in.AllowPrivate, in.Enabled, now.UTC(), now.UTC())
	if err != nil {
		return fmt.Errorf("webhooks: create endpoint: %w", err)
	}
	return nil
}

func (s *WebhookStore) UpdateEndpoint(ctx context.Context, in webhooks.EndpointInput, now time.Time) error {
	var (
		res sql.Result
		err error
	)
	if in.SecretCiphertext == "" {
		res, err = s.db.ExecContext(ctx, `
			UPDATE webhook_endpoints
			SET name = $1, url = $2, event_filter = $3, allow_private = $4, enabled = $5, updated_at = $6
			WHERE id = $7
		`, in.Name, in.URL, in.EventFilter, in.AllowPrivate, in.Enabled, now.UTC(), in.ID)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE webhook_endpoints
			SET name = $1, url = $2, secret_ciphertext = $3, event_filter = $4, allow_private = $5, enabled = $6, updated_at = $7
			WHERE id = $8
		`, in.Name, in.URL, in.SecretCiphertext, in.EventFilter, in.AllowPrivate, in.Enabled, now.UTC(), in.ID)
	}
	if err != nil {
		return fmt.Errorf("webhooks: update endpoint: %w", err)
	}
	return checkWebhookAffected(res)
}

func (s *WebhookStore) DeleteEndpoint(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("webhooks: delete endpoint: %w", err)
	}
	return checkWebhookAffected(res)
}

func (s *WebhookStore) GetEndpointMeta(ctx context.Context, id string) (webhooks.Endpoint, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, url, event_filter, allow_private, enabled
		FROM webhook_endpoints
		WHERE id = $1
	`, id)
	return scanWebhookEndpointMeta(row)
}

func (s *WebhookStore) ListEndpointMeta(ctx context.Context) ([]webhooks.Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, url, event_filter, allow_private, enabled
		FROM webhook_endpoints
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list endpoint meta: %w", err)
	}
	defer rows.Close()
	var out []webhooks.Endpoint
	for rows.Next() {
		ep, err := scanWebhookEndpointMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: list endpoint meta rows: %w", err)
	}
	return out, nil
}

type webhookRowScanner interface {
	Scan(dest ...any) error
}

func scanWebhookEndpointMeta(s webhookRowScanner) (webhooks.Endpoint, error) {
	var (
		ep           webhooks.Endpoint
		filterCSV    string
		allowPrivate bool
		enabled      bool
	)
	if err := s.Scan(&ep.ID, &ep.Name, &ep.URL, &filterCSV, &allowPrivate, &enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return webhooks.Endpoint{}, webhooks.ErrNotFound
		}
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: scan endpoint meta: %w", err)
	}
	ep.EventFilter = parseWebhookFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivate
	ep.Enabled = enabled
	return ep, nil
}
