package agents

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionManager_RegisterAssignsUniqueSequences(t *testing.T) {
	mgr := NewSessionManager()
	s1, _ := mgr.Register("agent-a", nil)
	s2, _ := mgr.Register("agent-b", nil)
	if s1.Sequence == s2.Sequence {
		t.Fatalf("expected distinct sequence numbers, got %d == %d", s1.Sequence, s2.Sequence)
	}
	if s1.Sequence == 0 || s2.Sequence == 0 {
		t.Fatalf("sequences must be non-zero: s1=%d s2=%d", s1.Sequence, s2.Sequence)
	}
}

func TestSessionManager_UnregisterOnlyRemovesOwnSession(t *testing.T) {
	mgr := NewSessionManager()
	_, unreg1 := mgr.Register("agent-a", nil)
	s2, _ := mgr.Register("agent-a", nil) // replaces

	// unreg1 must be a no-op now.
	unreg1()

	mgr.mu.RLock()
	current := mgr.sessions["agent-a"]
	mgr.mu.RUnlock()
	if current != s2 {
		t.Fatalf("stale unregister clobbered newer session")
	}
}

func TestSessionManager_NotifyCoalesces(t *testing.T) {
	mgr := NewSessionManager()
	s, _ := mgr.Register("agent-a", nil)
	mgr.Notify("agent-a")
	mgr.Notify("agent-a") // second notify must not block
	mgr.Notify("agent-a")
	select {
	case <-s.Wake:
	default:
		t.Fatalf("expected a wake signal to be queued")
	}
}

func TestSessionManager_NotifyAfterTerminateIsSafe(t *testing.T) {
	mgr := NewSessionManager()
	mgr.Register("agent-a", nil)
	if !mgr.Terminate("agent-a") {
		t.Fatalf("expected Terminate to report a deletion")
	}
	mgr.Notify("agent-a") // must not panic
}

func TestSessionManager_NotifyMany(t *testing.T) {
	mgr := NewSessionManager()
	sA, _ := mgr.Register("agent-a", nil)
	sB, _ := mgr.Register("agent-b", nil)

	mgr.NotifyMany([]string{"agent-a", "agent-a", "agent-b"})
	select {
	case <-sA.Wake:
	default:
		t.Fatalf("expected agent-a wake")
	}
	select {
	case <-sB.Wake:
	default:
		t.Fatalf("expected agent-b wake")
	}
}

func TestSessionManager_ConcurrentAccess(t *testing.T) {
	mgr := NewSessionManager()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, unreg := mgr.Register("agent", nil)
			unreg()
		}()
		go func() {
			defer wg.Done()
			mgr.Notify("agent")
		}()
	}
	wg.Wait()
}

func TestSessionRediscoverFlagTakeOnce(t *testing.T) {
	m := NewSessionManager()
	sess, unregister := m.Register("agent-1", nil)
	defer unregister()

	if sess.TakeRediscovery() {
		t.Fatal("fresh session should not have a pending rediscovery")
	}

	if !m.RequestRediscovery("agent-1") {
		t.Fatal("RequestRediscovery should report a live session")
	}

	if !sess.TakeRediscovery() {
		t.Fatal("flag should be set after RequestRediscovery")
	}
	if sess.TakeRediscovery() {
		t.Fatal("flag must be consumed exactly once")
	}
}

func TestRequestRediscoveryNoSession(t *testing.T) {
	m := NewSessionManager()
	if m.RequestRediscovery("missing") {
		t.Fatal("RequestRediscovery on unknown agent should return false")
	}
}

func TestRequestRediscoveryAllCountsAndWakes(t *testing.T) {
	m := NewSessionManager()
	a, ua := m.Register("a", nil)
	defer ua()
	b, ub := m.Register("b", nil)
	defer ub()

	if n := m.RequestRediscoveryAll(); n != 2 {
		t.Fatalf("RequestRediscoveryAll() = %d, want 2", n)
	}
	if !a.TakeRediscovery() || !b.TakeRediscovery() {
		t.Fatal("both sessions should have the flag set")
	}
}

// TestRegisterTerminatesSupersededSession guards B5: installing a replacement
// session for the same agent_id must force-terminate the previous one —
// close its Done AND cancel its connection ctx — so the superseded stream's
// goroutines (receive loop, dispatch ticker) stop immediately instead of
// running until their own Recv fails. Two live streams for one agent_id
// (cloned VM / copied state file) used to both dispatch jobs silently.
func TestRegisterTerminatesSupersededSession(t *testing.T) {
	mgr := NewSessionManager()
	var oldCancelled atomic.Bool
	old, _ := mgr.Register("agent-a", func() { oldCancelled.Store(true) })

	replacement, _ := mgr.Register("agent-a", nil)

	select {
	case <-old.Done:
	default:
		t.Fatal("superseded session Done must be closed on replacement")
	}
	if !oldCancelled.Load() {
		t.Fatal("superseded session's connection ctx must be cancelled")
	}
	select {
	case <-replacement.Done:
		t.Fatal("replacement session must stay live")
	default:
	}
}

// TestTerminateCancelsConnection pins the same contract for the operator
// path: Terminate must cancel the stream ctx, not just close Done.
func TestTerminateCancelsConnection(t *testing.T) {
	mgr := NewSessionManager()
	var cancelled atomic.Bool
	mgr.Register("agent-a", func() { cancelled.Store(true) })
	if !mgr.Terminate("agent-a") {
		t.Fatal("expected Terminate to report a deletion")
	}
	if !cancelled.Load() {
		t.Fatal("Terminate must cancel the session's connection ctx")
	}
}

// TestShouldWarnOnReplaceWindow pins the duplicate-identity heuristic.
func TestShouldWarnOnReplaceWindow(t *testing.T) {
	now := time.Now()
	fresh := &Session{RegisteredAt: now.Add(-5 * time.Second)}
	stale := &Session{RegisteredAt: now.Add(-2 * time.Minute)}
	if !shouldWarnOnReplace(fresh, now) {
		t.Fatal("replacement within the keepalive window must warn")
	}
	if shouldWarnOnReplace(stale, now) {
		t.Fatal("replacement after a normal reconnect cycle must not warn")
	}
}
