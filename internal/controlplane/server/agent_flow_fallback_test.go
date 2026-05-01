package server

import (
	"testing"
	"time"
)

// snapshotFallbackOps returns a copy of the fallback-state batch buffer's
// pending mutations. Test-only — exposes the internal slice so the
// applyFallbackStateTransitionLocked tests can assert what was enqueued
// without having to flush the buffer to the store and read it back. The
// caller receives a fresh slice so subsequent enqueues do not race with
// assertion.
func (b *batchBuffer[T]) snapshot() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]T, len(b.items))
	copy(out, b.items)
	return out
}

// pendingFallbackOps is a thin convenience wrapper around the typed buffer
// snapshot used by applyFallbackStateTransitionLocked tests.
func (w *storeBatchWriter) pendingFallbackOps() []fallbackStateOp {
	return w.fallbackState.snapshot()
}

// runtimeFor builds a minimal AgentRuntime with the three flags the
// classifier reads (UseMiddleProxy, MERuntimeReady, ME2DCFallbackEnabled).
// All other fields stay zero — the classifier only branches on these three.
func runtimeFor(useMiddleProxy, meReady, fallbackEnabled bool) AgentRuntime {
	return AgentRuntime{
		UseMiddleProxy:       useMiddleProxy,
		MERuntimeReady:       meReady,
		ME2DCFallbackEnabled: fallbackEnabled,
	}
}

// fallbackTestServer constructs a server-with-batch-writer pair via the
// shared sqlite helper, and resets the fallback buffer between scripted
// transitions so tests can assert per-step enqueue behaviour.
func fallbackTestServer(t *testing.T) *Server {
	t.Helper()
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	if server.batchWriter == nil {
		t.Fatal("test server has no batch writer; expected sqlite-backed wiring")
	}
	return server
}

// drainPending returns and clears the pending fallback ops queued since the
// last call. Each transition test step asserts on the delta produced by that
// step, not the cumulative buffer contents.
func drainPending(t *testing.T, w *storeBatchWriter) []fallbackStateOp {
	t.Helper()
	w.fallbackState.mu.Lock()
	defer w.fallbackState.mu.Unlock()
	ops := make([]fallbackStateOp, len(w.fallbackState.items))
	copy(ops, w.fallbackState.items)
	w.fallbackState.items = w.fallbackState.items[:0]
	return ops
}

// fallback flag combinations used in the transition tests. The classifier
// reads three booleans:
//
//	use_middle_proxy=false         -> ModeDirect
//	use_middle_proxy=true && me_runtime_ready=true   -> ModeME
//	use_middle_proxy=true && me_runtime_ready=false && me2dc_fallback=true   -> ModeFallback
//	use_middle_proxy=true && me_runtime_ready=false && me2dc_fallback=false  -> ModeMeDown
const (
	agentID = "agent-fallback-tx"
)

func TestApplyFallbackStateTransitionNoneToFallbackStampsAndEnqueues(t *testing.T) {
	server := fallbackTestServer(t)

	server.mu.Lock()
	server.agents[agentID] = Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.applyFallbackStateTransitionLocked(server.agents[agentID])
	stamped, ok := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if !ok {
		t.Fatal("fallbackEnteredAt[agent] not stamped after fresh fallback transition")
	}
	if time.Since(stamped) > 5*time.Second {
		t.Fatalf("fallbackEnteredAt = %v, want recent (within 5s of now)", stamped)
	}

	ops := drainPending(t, server.batchWriter)
	if len(ops) != 1 {
		t.Fatalf("pending fallback ops = %d, want 1", len(ops))
	}
	if ops[0].op != "put" {
		t.Fatalf("op = %q, want %q", ops[0].op, "put")
	}
	if ops[0].agentID != agentID {
		t.Fatalf("op agentID = %q, want %q", ops[0].agentID, agentID)
	}
	if !ops[0].enteredAt.Equal(stamped) {
		t.Fatalf("op enteredAt = %v, want %v (matches in-memory stamp)", ops[0].enteredAt, stamped)
	}
}

func TestApplyFallbackStateTransitionFallbackToFallbackIsIdempotent(t *testing.T) {
	server := fallbackTestServer(t)

	// First transition: stamp + enqueue.
	server.mu.Lock()
	agent := Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.agents[agentID] = agent
	server.applyFallbackStateTransitionLocked(agent)
	first := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	// Second transition with the same fallback flags: no change.
	time.Sleep(2 * time.Millisecond) // ensure time.Now() has moved forward
	server.mu.Lock()
	server.applyFallbackStateTransitionLocked(agent)
	second := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if !first.Equal(second) {
		t.Fatalf("entered-at re-stamped on idempotent fallback heartbeat: first=%v second=%v", first, second)
	}
	ops := drainPending(t, server.batchWriter)
	if len(ops) != 0 {
		t.Fatalf("pending fallback ops on idempotent fallback transition = %d, want 0", len(ops))
	}
}

func TestApplyFallbackStateTransitionFallbackToMEClearsAndEnqueuesDelete(t *testing.T) {
	server := fallbackTestServer(t)

	// Enter fallback first.
	server.mu.Lock()
	server.agents[agentID] = Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.applyFallbackStateTransitionLocked(server.agents[agentID])
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	// Now the ME pool comes back: use_middle_proxy=true, me_runtime_ready=true.
	server.mu.Lock()
	healthy := Agent{ID: agentID, Runtime: runtimeFor(true, true, true)}
	server.agents[agentID] = healthy
	server.applyFallbackStateTransitionLocked(healthy)
	_, stillThere := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if stillThere {
		t.Fatal("fallbackEnteredAt[agent] retained after fallback->ME transition; want cleared")
	}
	ops := drainPending(t, server.batchWriter)
	if len(ops) != 1 {
		t.Fatalf("pending fallback ops after fallback->ME = %d, want 1", len(ops))
	}
	if ops[0].op != "delete" || ops[0].agentID != agentID {
		t.Fatalf("op = %+v, want {op:delete, agentID:%s}", ops[0], agentID)
	}
}

func TestApplyFallbackStateTransitionFallbackToDirectClearsAndEnqueuesDelete(t *testing.T) {
	server := fallbackTestServer(t)

	server.mu.Lock()
	server.agents[agentID] = Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.applyFallbackStateTransitionLocked(server.agents[agentID])
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	// Switch to direct mode: use_middle_proxy=false → ModeDirect.
	server.mu.Lock()
	direct := Agent{ID: agentID, Runtime: runtimeFor(false, false, false)}
	server.agents[agentID] = direct
	server.applyFallbackStateTransitionLocked(direct)
	_, stillThere := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if stillThere {
		t.Fatal("fallbackEnteredAt[agent] retained after fallback->direct transition; want cleared")
	}
	ops := drainPending(t, server.batchWriter)
	if len(ops) != 1 {
		t.Fatalf("pending fallback ops after fallback->direct = %d, want 1", len(ops))
	}
	if ops[0].op != "delete" {
		t.Fatalf("op = %+v, want delete", ops[0])
	}
}

func TestApplyFallbackStateTransitionFallbackToMeDownKeepsTimestamp(t *testing.T) {
	server := fallbackTestServer(t)

	// Enter fallback.
	server.mu.Lock()
	server.agents[agentID] = Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.applyFallbackStateTransitionLocked(server.agents[agentID])
	original := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	// Operator flips off me2dc_fallback while ME is still down — this is the
	// regression: previously the default branch deleted the entry.
	server.mu.Lock()
	meDown := Agent{ID: agentID, Runtime: runtimeFor(true, false, false)}
	server.agents[agentID] = meDown
	server.applyFallbackStateTransitionLocked(meDown)
	stillThere, ok := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if !ok {
		t.Fatal("fallbackEnteredAt[agent] cleared on fallback->me_down transition; want preserved")
	}
	if !stillThere.Equal(original) {
		t.Fatalf("fallbackEnteredAt rewritten: got %v, want %v (must be preserved as-is)", stillThere, original)
	}
	ops := drainPending(t, server.batchWriter)
	if len(ops) != 0 {
		t.Fatalf("pending fallback ops on fallback->me_down = %d, want 0 (no enqueue)", len(ops))
	}
}

func TestApplyFallbackStateTransitionMeDownToFallbackKeepsOriginalTimestamp(t *testing.T) {
	server := fallbackTestServer(t)

	// Enter fallback first to stamp the original entered-at, then drift to
	// me_down (timestamp preserved by Issue 1 fix), then flip back to fallback.
	server.mu.Lock()
	server.agents[agentID] = Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.applyFallbackStateTransitionLocked(server.agents[agentID])
	original := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	server.mu.Lock()
	meDown := Agent{ID: agentID, Runtime: runtimeFor(true, false, false)}
	server.agents[agentID] = meDown
	server.applyFallbackStateTransitionLocked(meDown)
	server.mu.Unlock()
	_ = drainPending(t, server.batchWriter)

	// Flag back on. Should be a no-op because hadPrev is true.
	time.Sleep(2 * time.Millisecond)
	server.mu.Lock()
	back := Agent{ID: agentID, Runtime: runtimeFor(true, false, true)}
	server.agents[agentID] = back
	server.applyFallbackStateTransitionLocked(back)
	got := server.fallbackEnteredAt[agentID]
	server.mu.Unlock()

	if !got.Equal(original) {
		t.Fatalf("entered-at re-stamped on me_down->fallback: got %v, want %v", got, original)
	}
	ops := drainPending(t, server.batchWriter)
	if len(ops) != 0 {
		t.Fatalf("pending fallback ops on me_down->fallback = %d, want 0", len(ops))
	}
}
