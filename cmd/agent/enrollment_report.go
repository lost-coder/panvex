package main

import (
	"context"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// localEvent is one buffered enrollment step recorded on the agent before the
// gRPC stream is up. We hold them in memory and flush after the first sync so
// the panel-side timeline captures the agent-side stages (cert persisted,
// gateway dialed, tls handshake ok) that the panel can never observe directly.
type localEvent struct {
	step    string
	level   string
	ts      time.Time
	message string
	fields  map[string]string
}

// enrollmentReporter is the agent-side collector that buffers local
// enrollment timeline events and ships them to the panel via
// ReportEnrollmentSteps once the connection is up.
//
// Safe for concurrent use: every external call takes mu. A reporter with an
// empty attemptID silently drops Record / Flush calls so callers do not have
// to nil-check on every step.
type enrollmentReporter struct {
	mu        sync.Mutex
	attemptID string
	events    []localEvent
}

// newEnrollmentReporter constructs an empty reporter. The caller must Bind an
// attempt id before any Record call has effect.
func newEnrollmentReporter() *enrollmentReporter { return &enrollmentReporter{} }

// Bind associates the reporter with a panel-side enrollment attempt. Empty
// attemptID is allowed (treated as "reporting disabled"). Resetting the
// attempt id discards any previously buffered events because they belong to
// the prior attempt.
func (r *enrollmentReporter) Bind(attemptID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attemptID = attemptID
	r.events = r.events[:0]
}

// Record appends one step to the buffer. No-op when the reporter is nil or
// no attempt id has been bound — keeps the call-site cheap inside hot dial
// / handshake paths.
func (r *enrollmentReporter) Record(step, level, message string, fields map[string]string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attemptID == "" {
		return
	}
	r.events = append(r.events, localEvent{
		step:    step,
		level:   level,
		ts:      time.Now().UTC(),
		message: message,
		fields:  fields,
	})
}

// RecordAt is like Record but stamps the event with the caller-supplied
// timestamp. Used to back-date agent_persisted_cert with the bootstrap's
// disk-write time after the runtime starts a fresh process.
func (r *enrollmentReporter) RecordAt(step, level, message string, ts time.Time, fields map[string]string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attemptID == "" {
		return
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	r.events = append(r.events, localEvent{
		step:    step,
		level:   level,
		ts:      ts.UTC(),
		message: message,
		fields:  fields,
	})
}

// Flush sends the buffered events to the panel and clears the buffer on
// success. A flush failure is non-fatal: the events stay in the buffer so a
// subsequent reconnect can retry. No-op when the buffer is empty, the
// reporter is nil, or no attempt id is bound.
func (r *enrollmentReporter) Flush(ctx context.Context, client gatewayrpc.AgentGatewayClient) error {
	if r == nil || client == nil {
		return nil
	}
	r.mu.Lock()
	if r.attemptID == "" || len(r.events) == 0 {
		r.mu.Unlock()
		return nil
	}
	attemptID := r.attemptID
	out := make([]*gatewayrpc.AgentEnrollmentEvent, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, &gatewayrpc.AgentEnrollmentEvent{
			Step:    e.step,
			Level:   e.level,
			Message: e.message,
			Ts:      timestamppb.New(e.ts),
			Fields:  e.fields,
		})
	}
	r.mu.Unlock()

	if _, err := client.ReportEnrollmentSteps(ctx, &gatewayrpc.ReportEnrollmentStepsRequest{
		AttemptId: attemptID,
		Events:    out,
	}); err != nil {
		return err
	}

	r.mu.Lock()
	r.events = r.events[:0]
	r.mu.Unlock()
	return nil
}
