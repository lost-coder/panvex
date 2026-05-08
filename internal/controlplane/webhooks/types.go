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
type Storage interface {
	ListEnabledEndpoints(ctx context.Context) ([]Endpoint, error)
	InsertOutbox(ctx context.Context, row OutboxRow) error
	ClaimReady(ctx context.Context, now time.Time, max int) ([]Delivery, error)
	MarkDelivered(ctx context.Context, id string, deliveredAt time.Time) error
	MarkFailed(ctx context.Context, id string, attempt int, nextAttempt time.Time, errMsg string, dead bool) error
}

// ErrNotFound is returned by Mark* when the row id no longer exists
// (e.g. the endpoint was deleted via cascade). Callers treat it as a
// no-op, not a failure.
var ErrNotFound = errors.New("webhooks: outbox row not found")
