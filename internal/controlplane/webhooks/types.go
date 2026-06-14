package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Event is what an event source hands to the producer. Action is a
// dot-namespaced label ("agent.unhealthy", "audit.user.login"); the
// receiver gets it both as the X-Panvex-Event header and inside the
// payload if the source nested it.
type Event struct {
	Action  string
	Payload json.RawMessage
}

// Endpoint describes one configured webhook receiver. Secret is the
// decrypted HMAC key — the storage layer is responsible for round-
// tripping the ciphertext to plaintext via secret-vault before
// handing the endpoint to the worker. EventFilter is a list of
// dot-prefix patterns ("agent.*", "audit.security.*"); empty list
// matches every event.
type Endpoint struct {
	ID           string
	Name         string
	URL          string
	Secret       []byte
	EventFilter  []string
	AllowPrivate bool
	Enabled      bool
}

// OutboxRow is one pending delivery. The producer creates it; the
// worker mutates Attempt / NextAttemptAt / LastError / Dead /
// DeliveredAt. Payload is exactly the bytes the receiver will see —
// canonicalisation is the producer's job.
type OutboxRow struct {
	ID            string
	EndpointID    string
	EventAction   string
	Payload       json.RawMessage
	Attempt       int
	NextAttemptAt time.Time
	LastError     string
	Dead          bool
	CreatedAt     time.Time
	DeliveredAt   *time.Time
}

// Delivery is ClaimReady's output: the outbox row joined with the
// endpoint metadata the worker needs to actually send. Exposed as a
// single type so the storage backend can do the join in one query
// and the worker doesn't roundtrip per-row.
type Delivery struct {
	Outbox   OutboxRow
	Endpoint Endpoint
}

// EndpointInput is the operator-supplied form for create / update.
// SecretCiphertext is the already-vault-encrypted form of the HMAC
// key — handlers do the encryption before calling the store so the
// plaintext never lands in storage code. EventFilter uses CSV
// representation (the same format webhook_endpoints stores).
type EndpointInput struct {
	ID               string // ignored on Update; assigned by Create
	Name             string
	URL              string
	SecretCiphertext string // pass empty to leave the existing secret on Update
	EventFilter      string // CSV; empty = match all
	AllowPrivate     bool
	Enabled          bool
}

// Storage is the seam between webhooks and persistent state. The
// in-memory implementation in memstore_test.go is the testing
// fixture; real backends live in
// internal/controlplane/storage/{postgres,sqlite}/webhooks.go.
//
// Implementations MUST:
//   - keep ListEnabledEndpoints idempotent within a single transaction
//   - return Endpoint.Secret already decrypted (no ciphertext leakage)
//   - make ClaimReady safe under concurrent workers (FOR UPDATE SKIP
//     LOCKED on Postgres, single-claim with serialised WAL writes on
//     SQLite)
//   - write CreatedAt / NextAttemptAt as the producer/worker provided
//     them — no clock drift on the DB side
//
// CRUD methods (CreateEndpoint / UpdateEndpoint / DeleteEndpoint /
// GetEndpoint / ListEndpoints) operate on the operator-facing
// representation and never expose plaintext secrets back — only the
// outbox-fan-out path returns Endpoint.Secret in plaintext, and only
// after the storage's injected SecretDecrypter has been invoked.
type Storage interface {
	ListEnabledEndpoints(ctx context.Context) ([]Endpoint, error)
	InsertOutbox(ctx context.Context, row OutboxRow) error
	ClaimReady(ctx context.Context, now time.Time, max int) ([]Delivery, error)
	MarkDelivered(ctx context.Context, id string, deliveredAt time.Time) error
	MarkFailed(ctx context.Context, id string, attempt int, nextAttempt time.Time, errMsg string, dead bool) error
	// PruneOutbox deletes terminal outbox rows: delivered rows whose
	// delivered_at, and dead rows whose created_at, precede the cutoff.
	// Pending (undelivered, non-dead) rows are never pruned (C4).
	// Returns the number of deleted rows.
	PruneOutbox(ctx context.Context, before time.Time) (int64, error)

	// Operator CRUD (does NOT decrypt — secrets stay at-rest).
	CreateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error
	UpdateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error
	DeleteEndpoint(ctx context.Context, id string) error
	// GetEndpointMeta returns the row's operator-visible fields
	// without the secret. SecretCiphertext is intentionally elided
	// from the returned Endpoint so handlers don't accidentally
	// echo it.
	GetEndpointMeta(ctx context.Context, id string) (Endpoint, error)
	// ListEndpointMeta is the admin view: includes disabled rows.
	// Same secret-elision rule as GetEndpointMeta.
	ListEndpointMeta(ctx context.Context) ([]Endpoint, error)
}

// ErrNotFound is returned by Mark* when the row id no longer exists
// (e.g. the endpoint was deleted via cascade). Callers treat it as a
// no-op, not a failure.
var ErrNotFound = errors.New("webhooks: outbox row not found")

// SecretDecrypter resolves an endpoint's stored ciphertext to the
// plaintext HMAC key. Real implementations close over a
// secretvault.Vault and the DomainWebhookSecret domain; tests pass
// a no-op (`func(s string) ([]byte, error) { return []byte(s), nil }`).
//
// The decrypter is owned by the storage backend, not the worker —
// this keeps webhooks.Storage's plaintext-Secret contract (documented
// on Endpoint) while letting the worker stay agnostic about how the
// secret was at-rest protected.
type SecretDecrypter func(ciphertext string) ([]byte, error)
