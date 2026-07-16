// Package kdf owns the project's Argon2id parameter profiles and is the
// single point where argon2.IDKey is invoked for password hashes and the
// CA-key blob.
//
// It exists for two reasons, both measured in
// docs/2026-07-16-resource-footprint.md:
//
//  1. Footprint. Every Argon2id derivation allocates MemoryKiB of block
//     array. The panel's startup peak was ~250 MB because several 96 MiB
//     derivations (admin bootstrap + CA key) ran back to back and their dead
//     buffers were not collected between calls. Serialising the calls and
//     forcing a GC while still holding the gate keeps the peak at roughly one
//     buffer instead of N.
//
//  2. Small devices. The `low-memory` profile switches NEW material to the
//     OWASP low-memory Argon2id configuration so the panel fits on a box with
//     a few hundred MB of RAM without abandoning a memory-hard KDF.
//
// The package is a leaf: it imports nothing from the rest of the control
// plane, so both auth and server may depend on it (see internal/controlplane/
// archguard).
package kdf

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"golang.org/x/crypto/argon2"
)

// Profile is one named Argon2id parameter set.
type Profile struct {
	Name      string
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
}

var (
	// Default is the project's hardened parameter set (Q3.U-S-16): well above
	// the OWASP minimum so a dictionary attack on a leaked hash or CA blob is
	// materially more expensive.
	Default = Profile{Name: "default", Time: 4, MemoryKiB: 96 * 1024, Threads: 2}
	// LowMemory is OWASP Argon2id configuration #2 (t=2, m=19 MiB, p=1) — the
	// documented minimum. Chosen for small devices where a 96 MiB buffer per
	// login is a real constraint.
	LowMemory = Profile{Name: "low-memory", Time: 2, MemoryKiB: 19456, Threads: 1}
)

var active atomic.Pointer[Profile]

func init() {
	p := Default
	active.Store(&p)
}

// SetActiveProfile selects the profile used for NEW password hashes and CA-key
// blobs. An empty name means "operator has no opinion" and maps to Default.
// An unknown name is an error and leaves the previous profile in place, so a
// typo in PANVEX_KDF_PROFILE fails the boot loudly rather than silently
// weakening or strengthening the KDF.
func SetActiveProfile(name string) error {
	switch name {
	case "", "default":
		p := Default
		active.Store(&p)
	case "low-memory":
		p := LowMemory
		active.Store(&p)
	default:
		return fmt.Errorf("unknown KDF profile %q (want default or low-memory)", name)
	}
	return nil
}

// Active returns the profile currently selected for new material.
func Active() Profile { return *active.Load() }

// gate admits one derivation at a time. Parallel calls would each allocate
// their own MemoryKiB block array (a login storm is then a memory-DoS), and
// sequential calls without a collection in between leave the previous buffer
// uncollected (the 250 MB startup peak).
var gate = make(chan struct{}, 1)

// deriveHook replaces the real derivation in tests.
var deriveHook func()

// derivations counts every derivation admitted through the gate. It exists so
// callers' tests can pin "exactly one derivation per verify call" (C-1: more
// than one parameter set per verify is a timing oracle that classifies the
// stored hash) without reaching into this package's internals.
var derivations atomic.Uint64

// Derivations returns the number of Argon2id derivations run since process
// start. Monotonic; intended for tests and diagnostics.
func Derivations() uint64 { return derivations.Load() }

// IDKey derives a key with Argon2id. It is the only argon2.IDKey call site for
// password hashes and the CA key: derivations are serialised on gate, and the
// block array of THIS call is collected before the next waiter allocates its
// own, so the resident peak stays at one buffer regardless of profile or
// concurrency.
//
// The trade-off is a queue on concurrent logins. That is deliberate: it is
// exactly the anti-DoS property we want, and Argon2id's own parallelism
// (Threads) still uses multiple cores within a single derivation.
func IDKey(password, salt []byte, time, memoryKiB uint32, threads uint8, keyLen uint32) []byte {
	gate <- struct{}{}
	// Released via defer so a panic inside the derivation cannot wedge the
	// gate and deadlock every subsequent login.
	defer func() { <-gate }()
	derivations.Add(1)
	if hook := deriveHook; hook != nil {
		hook()
		return make([]byte, keyLen)
	}
	out := argon2.IDKey(password, salt, time, memoryKiB, threads, keyLen)
	// Collect this call's block array while still holding the gate: the next
	// waiter must not allocate its buffer until ours is gone.
	runtime.GC()
	return out
}
