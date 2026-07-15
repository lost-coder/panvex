package clients

import (
	"context"
	"testing"
	"time"
)

// TestNameTaken covers the global-uniqueness query that backs the panel's
// client-name uniqueness enforcement (R10b Task 6 / C7): a name is "taken"
// only by a LIVING (non-deleted) client other than the excludeID.
func TestNameTaken(t *testing.T) {
	svc := newMirrorTestService()

	// A single living client named "alice".
	svc.MirrorReplaceInMemory(Client{ID: ClientID("c-1"), Name: "alice"}, nil, nil)

	// Its own name, excluding itself → free (rename-to-self is allowed).
	if svc.NameTaken("alice", ClientID("c-1")) {
		t.Fatal("NameTaken(alice, c-1) = true, want false (a client never blocks its own name)")
	}
	// Same name on create (excludeID "") → taken by the living client.
	if !svc.NameTaken("alice", "") {
		t.Fatal("NameTaken(alice, \"\") = false, want true (a living client holds this name)")
	}
	// A different, unused name → free.
	if svc.NameTaken("bob", "") {
		t.Fatal("NameTaken(bob, \"\") = true, want false (no client holds this name)")
	}

	// A second living client trying to take "alice" (its own id is c-2, which
	// does not hold the name) → taken.
	if !svc.NameTaken("alice", ClientID("c-2")) {
		t.Fatal("NameTaken(alice, c-2) = false, want true (another living client holds it)")
	}
}

// TestNameTakenTombstonedNameIsFree verifies a deleted client's name is
// released: a tombstone (DeletedAt != nil) must not block reuse, and an
// evicted client (removed from the mirror entirely) likewise frees its name.
func TestNameTakenTombstonedNameIsFree(t *testing.T) {
	svc := newMirrorTestService()

	deletedAt := time.Now().UTC()
	// A tombstoned client still present in the mirror.
	svc.MirrorReplaceInMemory(Client{ID: ClientID("c-1"), Name: "carol", DeletedAt: &deletedAt}, nil, nil)
	if svc.NameTaken("carol", "") {
		t.Fatal("NameTaken(carol, \"\") = true, want false (tombstoned client's name is free)")
	}

	// Same but via full Delete (evicts from the mirror + name index).
	svc.MirrorReplaceInMemory(Client{ID: ClientID("c-2"), Name: "dave"}, nil, nil)
	if !svc.NameTaken("dave", "") {
		t.Fatal("precondition: NameTaken(dave) should be true before delete")
	}
	if err := svc.Delete(context.Background(), ClientID("c-2")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if svc.NameTaken("dave", "") {
		t.Fatal("NameTaken(dave, \"\") = true after Delete, want false (deleted client's name is free)")
	}
}
