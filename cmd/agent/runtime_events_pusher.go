package main

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
)

// runtimeEventsPusher drains a runtime-event Buffer and pushes batches
// to the panel via the caller-supplied send function. Triggers:
//   - immediate (no batching) whenever `notify` fires (caller signals
//     this on Warn/Error append)
//   - every `tickInterval`: drains accumulated Info events
//
// The pusher maintains its own cursor (lastSent) so it doesn't re-send.
// On send error the cursor is not advanced past the failed batch — the
// events stay in the ring (subject to overflow) and will be retried on
// the next cycle.
type runtimeEventsPusher struct {
	buf          *runtimeevents.Buffer
	send         func([]runtimeevents.Event) error
	tickInterval time.Duration
	notify       <-chan struct{}
	lastSent     time.Time
}

// runtimeEventsBatchCap bounds the per-Send message size. The buffer
// default is 200, so an extreme burst still fans out across at most 4
// stream frames.
const runtimeEventsBatchCap = 50

func newRuntimeEventsPusher(
	buf *runtimeevents.Buffer,
	send func([]runtimeevents.Event) error,
	tickInterval time.Duration,
	notify <-chan struct{},
) *runtimeEventsPusher {
	return &runtimeEventsPusher{
		buf:          buf,
		send:         send,
		tickInterval: tickInterval,
		notify:       notify,
	}
}

func (p *runtimeEventsPusher) Run(ctx context.Context) {
	ticker := time.NewTicker(p.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.flush(ctx)
		case <-p.notify:
			p.flush(ctx)
		}
	}
}

func (p *runtimeEventsPusher) flush(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	pending := p.buf.DrainSince(p.lastSent)
	if len(pending) == 0 {
		return
	}
	for start := 0; start < len(pending); start += runtimeEventsBatchCap {
		end := start + runtimeEventsBatchCap
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]
		if err := p.send(batch); err != nil {
			// Stop on send error; buffer retains events, future cycle retries.
			return
		}
		p.lastSent = batch[len(batch)-1].Ts
	}
}
