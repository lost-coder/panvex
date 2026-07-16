package kdf

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

// setDeriveHookForTest swaps the real Argon2id derivation for f and returns a
// restore func. Tests must not run in parallel while a hook is installed.
func setDeriveHookForTest(f func()) (restore func()) {
	prev := deriveHook
	deriveHook = f
	return func() { deriveHook = prev }
}

func TestSetActiveProfile(t *testing.T) {
	t.Cleanup(func() { _ = SetActiveProfile("") })
	if err := SetActiveProfile(""); err != nil || Active().Name != "default" {
		t.Fatalf("empty must mean default, got %v/%s", err, Active().Name)
	}
	if err := SetActiveProfile("default"); err != nil || Active().MemoryKiB != 96*1024 {
		t.Fatalf("default: %v/%d", err, Active().MemoryKiB)
	}
	if err := SetActiveProfile("low-memory"); err != nil || Active().MemoryKiB != 19456 {
		t.Fatalf("low-memory: %v/%d", err, Active().MemoryKiB)
	}
	if err := SetActiveProfile("bogus"); err == nil {
		t.Fatal("unknown profile must error")
	}
	// A rejected profile must not disturb the previously active one.
	if Active().Name != "low-memory" {
		t.Fatalf("rejected profile must not change active, got %s", Active().Name)
	}
}

// TestIDKeySerializesDerivations pins the anti-DoS invariant: concurrent
// callers never hold two Argon2 block arrays at once, so a login storm
// cannot stack N × MemoryKiB buffers.
func TestIDKeySerializesDerivations(t *testing.T) {
	var current, peak atomic.Int32
	restore := setDeriveHookForTest(func() {
		v := current.Add(1)
		for {
			old := peak.Load()
			if v <= old || peak.CompareAndSwap(old, v) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		current.Add(-1)
	})
	defer restore()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			IDKey([]byte("pw"), []byte("0123456789abcdef"), 1, 64, 1, 32)
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Fatalf("derivations must be serialized, peak concurrency = %d", peak.Load())
	}
}

// TestIDKeyDerivesRealArgon2 guards against the gate/GC wrapper silently
// changing the derived bytes: the output must equal a direct argon2.IDKey
// call with the same inputs, and be deterministic.
func TestIDKeyDerivesRealArgon2(t *testing.T) {
	salt := []byte("0123456789abcdef")
	got := IDKey([]byte("pw"), salt, 1, 64, 1, 32)
	// Compare against the upstream primitive directly, NOT against another
	// IDKey call: comparing the wrapper with itself would pass even if it
	// derived something that is not Argon2id at all.
	want := argon2.IDKey([]byte("pw"), salt, 1, 64, 1, 32)
	if len(got) != 32 {
		t.Fatalf("key length = %d, want 32", len(got))
	}
	if string(got) != string(want) {
		t.Fatal("IDKey must return exactly what argon2.IDKey returns for the same inputs")
	}
	other := IDKey([]byte("pw2"), salt, 1, 64, 1, 32)
	if string(got) == string(other) {
		t.Fatal("IDKey must depend on the password")
	}
}

// TestIDKeyReleasesGateOnPanic ensures a panicking derivation cannot wedge
// the gate and deadlock every later login.
func TestIDKeyReleasesGateOnPanic(t *testing.T) {
	restore := setDeriveHookForTest(func() { panic("boom") })
	func() {
		defer func() { _ = recover() }()
		IDKey([]byte("pw"), []byte("0123456789abcdef"), 1, 64, 1, 32)
	}()
	restore()

	done := make(chan struct{})
	go func() {
		IDKey([]byte("pw"), []byte("0123456789abcdef"), 1, 64, 1, 32)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gate was not released after a panicking derivation")
	}
}
