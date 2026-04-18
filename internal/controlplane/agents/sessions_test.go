package agents

import (
	"sync"
	"testing"
)

func TestSessionManager_RegisterAssignsUniqueSequences(t *testing.T) {
	mgr := NewSessionManager()
	s1, _ := mgr.Register("agent-a")
	s2, _ := mgr.Register("agent-b")
	if s1.Sequence == s2.Sequence {
		t.Fatalf("expected distinct sequence numbers, got %d == %d", s1.Sequence, s2.Sequence)
	}
	if s1.Sequence == 0 || s2.Sequence == 0 {
		t.Fatalf("sequences must be non-zero: s1=%d s2=%d", s1.Sequence, s2.Sequence)
	}
}

func TestSessionManager_UnregisterOnlyRemovesOwnSession(t *testing.T) {
	mgr := NewSessionManager()
	_, unreg1 := mgr.Register("agent-a")
	s2, _ := mgr.Register("agent-a") // replaces

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
	s, _ := mgr.Register("agent-a")
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
	mgr.Register("agent-a")
	if !mgr.Terminate("agent-a") {
		t.Fatalf("expected Terminate to report a deletion")
	}
	mgr.Notify("agent-a") // must not panic
}

func TestSessionManager_NotifyMany(t *testing.T) {
	mgr := NewSessionManager()
	sA, _ := mgr.Register("agent-a")
	sB, _ := mgr.Register("agent-b")

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
			_, unreg := mgr.Register("agent")
			unreg()
		}()
		go func() {
			defer wg.Done()
			mgr.Notify("agent")
		}()
	}
	wg.Wait()
}
