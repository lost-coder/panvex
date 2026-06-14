package sqlite

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

// WebhookStore implements webhooks.Storage on top of an existing
// sqlite *sql.DB pool. It does not embed sqlite.Store (the main
// storage struct) on purpose: webhooks deliveries are independent of
// the main control-plane state machine, and bundling them avoids
// the existing transaction-bound dbExecutor abstraction. A future
// refactor that adds an outbox-write to the same tx as an event
// source can promote the methods to Store; until then the
// dependency surface stays small.
type WebhookStore struct {
	db      *sql.DB
	decrypt webhooks.SecretDecrypter
}

// NewWebhookStore wires a webhook storage backend over the given
// pool. decrypt is invoked with the raw secret_ciphertext column
// value and must return the plaintext HMAC key — typically a closure
// that calls secretvault.Vault.Decrypt(DomainWebhookSecret, …).
//
// nil decrypt is treated as identity (returns ciphertext as bytes),
// matching the secretvault no-op behaviour for dev installs without
// an encryption key. Production must always pass a real decrypter.
func NewWebhookStore(db *sql.DB, decrypt webhooks.SecretDecrypter) *WebhookStore {
	if decrypt == nil {
		decrypt = func(s string) ([]byte, error) { return []byte(s), nil }
	}
	return &WebhookStore{db: db, decrypt: decrypt}
}

// ListEnabledEndpoints returns every enabled webhook_endpoints row.
// Disabled rows are filtered server-side so the worker doesn't
// shuttle them over the network just to drop them. Secrets are
// decrypted before return per the Storage contract.
func (s *WebhookStore) ListEnabledEndpoints(ctx context.Context) ([]webhooks.Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, url, secret_ciphertext, event_filter, allow_private, enabled
		FROM webhook_endpoints
		WHERE enabled = 1
	`)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list endpoints: %w", err)
	}
	defer rows.Close()
	var out []webhooks.Endpoint
	for rows.Next() {
		ep, err := scanEndpoint(rows, s.decrypt)
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

// InsertOutbox creates one pending delivery. Caller-supplied ID
// must be unique; SQLite's PK constraint surfaces a duplicate as
// a generic constraint error.
func (s *WebhookStore) InsertOutbox(ctx context.Context, row webhooks.OutboxRow) error {
	payload := row.Payload
	if len(payload) == 0 {
		// Guarantee valid JSON in the column even when the producer
		// passed nil; receivers expect a JSON body.
		payload = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_outbox
			(id, endpoint_id, event_action, payload, attempt, next_attempt_at, last_error, dead, created_at)
		VALUES
			(?, ?, ?, ?, ?, ?, '', 0, ?)
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

// ClaimReady selects up to max ready rows joined with their
// endpoint and returns Delivery records. SQLite's WAL gives one
// writer at a time, so a worker reading then writing within a
// short window is contention-safe; for multi-worker deployments,
// promote to a transactional UPDATE … RETURNING pattern.
func (s *WebhookStore) ClaimReady(ctx context.Context, now time.Time, max int) ([]webhooks.Delivery, error) {
	if max <= 0 {
		max = 32
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			o.id, o.endpoint_id, o.event_action, o.payload,
			o.attempt, o.next_attempt_at, o.last_error, o.dead,
			o.created_at, o.delivered_at,
			e.id, e.name, e.url, e.secret_ciphertext, e.event_filter,
			e.allow_private, e.enabled
		FROM webhook_outbox o
		INNER JOIN webhook_endpoints e ON e.id = o.endpoint_id
		WHERE o.dead = 0
		  AND o.delivered_at IS NULL
		  AND o.next_attempt_at <= ?
		  AND e.enabled = 1
		ORDER BY o.next_attempt_at ASC, o.id ASC
		LIMIT ?
	`, now.UTC(), max)
	if err != nil {
		return nil, fmt.Errorf("webhooks: claim ready: %w", err)
	}
	defer rows.Close()

	var out []webhooks.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows, s.decrypt)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: claim ready rows: %w", err)
	}
	return out, nil
}

// MarkDelivered records a successful 2xx delivery. The row stays in
// the table so operators can audit; pruned by the timeseries-rollup
// worker via PruneOutbox.
func (s *WebhookStore) MarkDelivered(ctx context.Context, id string, deliveredAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhook_outbox
		SET delivered_at = ?, last_error = ''
		WHERE id = ?
	`, deliveredAt.UTC(), id)
	if err != nil {
		return fmt.Errorf("webhooks: mark delivered: %w", err)
	}
	return checkAffected(res)
}

// MarkFailed updates the row after a non-2xx / transport failure.
// dead=true permanently retires the row.
func (s *WebhookStore) MarkFailed(ctx context.Context, id string, attempt int, nextAttempt time.Time, errMsg string, dead bool) error {
	deadInt := 0
	if dead {
		deadInt = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhook_outbox
		SET attempt = ?, next_attempt_at = ?, last_error = ?, dead = ?
		WHERE id = ?
	`, attempt, nextAttempt.UTC(), errMsg, deadInt, id)
	if err != nil {
		return fmt.Errorf("webhooks: mark failed: %w", err)
	}
	return checkAffected(res)
}

// PruneOutbox deletes terminal rows per the webhooks.Storage contract.
// Delivered rows age out by delivered_at, dead rows by created_at;
// live pending rows are never touched.
func (s *WebhookStore) PruneOutbox(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM webhook_outbox
		WHERE (delivered_at IS NOT NULL AND delivered_at < ?)
		   OR (dead = 1 AND created_at < ?)
	`, before.UTC(), before.UTC())
	if err != nil {
		return 0, fmt.Errorf("webhooks: prune outbox: %w", err)
	}
	return res.RowsAffected()
}

// scanEndpoint reads one webhook_endpoints row and decrypts the
// secret. Used by both ListEnabledEndpoints and (via scanDelivery)
// ClaimReady.
func scanEndpoint(s *sql.Rows, decrypt webhooks.SecretDecrypter) (webhooks.Endpoint, error) {
	var (
		ep              webhooks.Endpoint
		secretCipher    string
		filterCSV       string
		allowPrivateInt int
		enabledInt      int
	)
	if err := s.Scan(&ep.ID, &ep.Name, &ep.URL, &secretCipher, &filterCSV, &allowPrivateInt, &enabledInt); err != nil {
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: scan endpoint: %w", err)
	}
	plain, err := decrypt(secretCipher)
	if err != nil {
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: decrypt endpoint %q secret: %w", ep.ID, err)
	}
	ep.Secret = plain
	ep.EventFilter = parseFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivateInt != 0
	ep.Enabled = enabledInt != 0
	return ep, nil
}

func scanDelivery(s *sql.Rows, decrypt webhooks.SecretDecrypter) (webhooks.Delivery, error) {
	var (
		row             webhooks.OutboxRow
		payloadStr      string
		nextAttempt     time.Time
		createdAt       time.Time
		deliveredAt     sql.NullTime
		deadInt         int
		ep              webhooks.Endpoint
		secretCipher    string
		filterCSV       string
		allowPrivateInt int
		enabledInt      int
	)
	if err := s.Scan(
		&row.ID, &row.EndpointID, &row.EventAction, &payloadStr,
		&row.Attempt, &nextAttempt, &row.LastError, &deadInt,
		&createdAt, &deliveredAt,
		&ep.ID, &ep.Name, &ep.URL, &secretCipher, &filterCSV,
		&allowPrivateInt, &enabledInt,
	); err != nil {
		return webhooks.Delivery{}, fmt.Errorf("webhooks: scan delivery: %w", err)
	}
	row.Payload = json.RawMessage(payloadStr)
	row.NextAttemptAt = nextAttempt.UTC()
	row.CreatedAt = createdAt.UTC()
	row.Dead = deadInt != 0
	if deliveredAt.Valid {
		t := deliveredAt.Time.UTC()
		row.DeliveredAt = &t
	}
	plain, err := decrypt(secretCipher)
	if err != nil {
		return webhooks.Delivery{}, fmt.Errorf("webhooks: decrypt endpoint %q secret: %w", ep.ID, err)
	}
	ep.Secret = plain
	ep.EventFilter = parseFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivateInt != 0
	ep.Enabled = enabledInt != 0
	return webhooks.Delivery{Outbox: row, Endpoint: ep}, nil
}

func parseFilterCSV(csv string) []string {
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

func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhooks: rows affected: %w", err)
	}
	if n == 0 {
		// Treat missing rows as a soft no-op: an endpoint cascade-
		// delete may have removed the outbox row between claim and
		// mark. Surface ErrNotFound so the worker can log and move
		// on rather than crashing the loop.
		return webhooks.ErrNotFound
	}
	return nil
}

// CreateEndpoint inserts a new operator-defined receiver. The
// caller has already vault-encrypted the HMAC key into
// SecretCiphertext.
func (s *WebhookStore) CreateEndpoint(ctx context.Context, in webhooks.EndpointInput, now time.Time) error {
	allowInt := 0
	if in.AllowPrivate {
		allowInt = 1
	}
	enabledInt := 0
	if in.Enabled {
		enabledInt = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_endpoints
			(id, name, url, secret_ciphertext, event_filter, allow_private, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, in.ID, in.Name, in.URL, in.SecretCiphertext, in.EventFilter, allowInt, enabledInt, now.UTC(), now.UTC())
	if err != nil {
		return fmt.Errorf("webhooks: create endpoint: %w", err)
	}
	return nil
}

// UpdateEndpoint replaces an existing row's operator-visible fields.
// Empty SecretCiphertext leaves the existing secret untouched —
// rotating the secret is opt-in (the operator must explicitly send
// a new value) so an accidental form-submission with the field
// blanked doesn't silently break HMAC verification on every
// receiver.
func (s *WebhookStore) UpdateEndpoint(ctx context.Context, in webhooks.EndpointInput, now time.Time) error {
	allowInt := 0
	if in.AllowPrivate {
		allowInt = 1
	}
	enabledInt := 0
	if in.Enabled {
		enabledInt = 1
	}
	var (
		res sql.Result
		err error
	)
	if in.SecretCiphertext == "" {
		res, err = s.db.ExecContext(ctx, `
			UPDATE webhook_endpoints
			SET name = ?, url = ?, event_filter = ?, allow_private = ?, enabled = ?, updated_at = ?
			WHERE id = ?
		`, in.Name, in.URL, in.EventFilter, allowInt, enabledInt, now.UTC(), in.ID)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE webhook_endpoints
			SET name = ?, url = ?, secret_ciphertext = ?, event_filter = ?, allow_private = ?, enabled = ?, updated_at = ?
			WHERE id = ?
		`, in.Name, in.URL, in.SecretCiphertext, in.EventFilter, allowInt, enabledInt, now.UTC(), in.ID)
	}
	if err != nil {
		return fmt.Errorf("webhooks: update endpoint: %w", err)
	}
	return checkAffected(res)
}

// DeleteEndpoint removes the endpoint row; ON DELETE CASCADE on
// webhook_outbox cleans up any pending deliveries.
func (s *WebhookStore) DeleteEndpoint(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("webhooks: delete endpoint: %w", err)
	}
	return checkAffected(res)
}

// GetEndpointMeta returns the operator-visible fields without the
// secret ciphertext. The secret never leaves storage on the read
// path — only the worker's outbox claim path joins it for HMAC
// signing, decrypted via the SecretDecrypter at that point.
func (s *WebhookStore) GetEndpointMeta(ctx context.Context, id string) (webhooks.Endpoint, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, url, event_filter, allow_private, enabled
		FROM webhook_endpoints
		WHERE id = ?
	`, id)
	return scanEndpointMeta(row)
}

// ListEndpointMeta returns every endpoint (including disabled) in
// stable order — admin view for the operator UI.
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
		ep, err := scanEndpointMetaRow(rows)
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

// rowScanner is the subset of *sql.Row / *sql.Rows the scan helpers
// need — lets meta-row scanning reuse one routine across single-row
// and many-row variants.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEndpointMeta(s rowScanner) (webhooks.Endpoint, error) {
	var (
		ep              webhooks.Endpoint
		filterCSV       string
		allowPrivateInt int
		enabledInt      int
	)
	if err := s.Scan(&ep.ID, &ep.Name, &ep.URL, &filterCSV, &allowPrivateInt, &enabledInt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return webhooks.Endpoint{}, webhooks.ErrNotFound
		}
		return webhooks.Endpoint{}, fmt.Errorf("webhooks: scan endpoint meta: %w", err)
	}
	ep.EventFilter = parseFilterCSV(filterCSV)
	ep.AllowPrivate = allowPrivateInt != 0
	ep.Enabled = enabledInt != 0
	return ep, nil
}

func scanEndpointMetaRow(rows *sql.Rows) (webhooks.Endpoint, error) { return scanEndpointMeta(rows) }
