package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Producer fans an event out into one outbox row per matching
// endpoint. The producer is intentionally minimal — no queueing,
// no dedupe — so it can be called in the same DB transaction as the
// event source's existing write (atomic-with-the-fact-that-happened
// is the durability contract).
type Producer struct {
	storage Storage
	clock   func() time.Time
	idFunc  func() string
}

// NewProducer wires a producer with sensible defaults. clock and
// idFunc are exposed via setters for tests; nil arguments fall back
// to time.Now and a 16-byte random hex.
func NewProducer(storage Storage) *Producer {
	return &Producer{
		storage: storage,
		clock:   time.Now,
		idFunc:  newRandomID,
	}
}

// SetClock injects a deterministic time source. Tests use this to
// freeze NextAttemptAt / CreatedAt for round-trip equality checks.
func (p *Producer) SetClock(clock func() time.Time) {
	if clock != nil {
		p.clock = clock
	}
}

// SetIDFunc injects a deterministic ID generator. Tests use this
// to assert exactly which row IDs landed in the outbox.
func (p *Producer) SetIDFunc(idFunc func() string) {
	if idFunc != nil {
		p.idFunc = idFunc
	}
}

// Publish writes one outbox row per enabled endpoint whose filter
// matches ev.Action. Returns nil with no rows written if no
// endpoint matched (event sources can call Publish unconditionally;
// the lookup cost is one query). Publish does not retry — if the
// storage write fails, the caller's transaction must roll back so
// the originating event also rolls back.
func (p *Producer) Publish(ctx context.Context, ev Event) error {
	if ev.Action == "" {
		return fmt.Errorf("webhooks: event Action is empty")
	}
	endpoints, err := p.storage.ListEnabledEndpoints(ctx)
	if err != nil {
		return fmt.Errorf("webhooks: list endpoints: %w", err)
	}
	now := p.clock().UTC()
	for _, ep := range endpoints {
		if !ep.Enabled {
			continue
		}
		if !matchesFilter(ev.Action, ep.EventFilter) {
			continue
		}
		row := OutboxRow{
			ID:            p.idFunc(),
			EndpointID:    ep.ID,
			EventAction:   ev.Action,
			Payload:       ev.Payload,
			Attempt:       0,
			NextAttemptAt: now,
			CreatedAt:     now,
		}
		if err := p.storage.InsertOutbox(ctx, row); err != nil {
			return fmt.Errorf("webhooks: insert outbox row: %w", err)
		}
	}
	return nil
}

// newRandomID returns a 32-hex-char (16 byte) random identifier.
// Used as both the outbox row ID and the X-Panvex-Delivery header
// the receiver sees. Long enough to dedupe at the receiver without
// a counter; short enough to log inline.
func newRandomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
