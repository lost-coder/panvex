package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestApplyBulkClientEnable_NotFoundIsNotRetryable proves the baseline
// (already-correct) case: a bulk id that genuinely does not exist is
// reported as a plain not-found failure with Retryable left false (the zero
// value, omitted from the wire payload).
func TestApplyBulkClientEnable_NotFoundIsNotRetryable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	server, _ := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	request := bulkClientRequest{Action: bulkClientEnable, IDs: []string{"does-not-exist"}}
	response := &bulkClientResponse{Failed: make([]bulkClientFailure, 0)}
	server.applyBulkClientEnable(ctx, "user-000001", FleetScopeAccess{Global: true}, request, response)

	if len(response.Failed) != 1 {
		t.Fatalf("Failed = %v, want exactly 1 entry", response.Failed)
	}
	got := response.Failed[0]
	if got.Error != msgClientNotFound {
		t.Fatalf("Error = %q, want %q", got.Error, msgClientNotFound)
	}
	if got.Retryable {
		t.Fatal("Retryable = true for a genuine not-found; want false so the operator knows not to retry")
	}
}

// TestApplyBulkClientDelete_OperationalErrorIsRetryableNotNotFound is the
// 3.13 regression guard: when the lookup/mutation fails for an operational
// reason (here: the underlying store is closed out from under an in-flight
// mutation, simulating a real DB failure) rather than because the client
// does not exist, the bulk response must NOT report a plain not-found
// shape. It must be tagged Retryable so the operator can tell "try again"
// apart from "this id is gone for good", and it must not echo the raw
// storage error text (consistent with the 3.10 allowlist).
func TestApplyBulkClientDelete_OperationalErrorIsRetryableNotNotFound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	created, _, _, err := server.createClient(ctx, "user-000001", clientMutationInput{
		Name:          "delete-me",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient: %v", err)
	}

	// Force every subsequent store operation to fail with a generic
	// operational error (not storage.ErrNotFound) by closing the store
	// while the in-memory mirror (clientsSvc) still believes the client
	// exists — deleteClient's clientDetailSnapshot hit succeeds (mirror
	// lookup, no store access) but replaceClientStateWithContext's
	// SaveState call hits the now-closed store and fails operationally.
	if err := server.store.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}

	request := bulkClientRequest{Action: bulkClientDelete, IDs: []string{string(created.ID)}}
	response := &bulkClientResponse{Failed: make([]bulkClientFailure, 0)}
	server.applyBulkClientDelete(ctx, "user-000001", FleetScopeAccess{Global: true}, request, response)

	if len(response.Succeeded) != 0 {
		t.Fatalf("Succeeded = %v, want none (store is closed)", response.Succeeded)
	}
	if len(response.Failed) != 1 {
		t.Fatalf("Failed = %v, want exactly 1 entry", response.Failed)
	}
	got := response.Failed[0]
	if got.Error == msgClientNotFound {
		t.Fatalf("Error reported as not-found (%q) for an operational failure — operator cannot tell this apart from a genuinely missing client", got.Error)
	}
	if !got.Retryable {
		t.Fatal("Retryable = false for an operational failure; want true so the operator knows a retry might succeed")
	}
	if strings.Contains(strings.ToLower(got.Error), "closed") || strings.Contains(got.Error, "sqlite") {
		t.Fatalf("Error leaked raw storage detail: %q", got.Error)
	}
}

// TestAppendBulkClientFailureFromError is a focused unit test of the
// classification helper itself: storage.ErrNotFound (wrapped or bare) must
// never be marked Retryable, and any other error must always be marked
// Retryable and must never surface via err.Error().
func TestAppendBulkClientFailureFromError(t *testing.T) {
	t.Parallel()

	t.Run("not found", func(t *testing.T) {
		response := &bulkClientResponse{Failed: make([]bulkClientFailure, 0)}
		appendBulkClientFailureFromError(response, "id-1", storage.ErrNotFound)
		if len(response.Failed) != 1 {
			t.Fatalf("Failed = %v, want 1 entry", response.Failed)
		}
		if response.Failed[0].Retryable {
			t.Fatal("ErrNotFound must not be Retryable")
		}
		if response.Failed[0].Error != msgClientNotFound {
			t.Fatalf("Error = %q, want %q", response.Failed[0].Error, msgClientNotFound)
		}
	})

	t.Run("operational error", func(t *testing.T) {
		response := &bulkClientResponse{Failed: make([]bulkClientFailure, 0)}
		opErr := errWithMessage("connection reset by peer: internal-detail-leak")
		appendBulkClientFailureFromError(response, "id-2", opErr)
		if len(response.Failed) != 1 {
			t.Fatalf("Failed = %v, want 1 entry", response.Failed)
		}
		if !response.Failed[0].Retryable {
			t.Fatal("operational error must be Retryable")
		}
		if response.Failed[0].Error != msgInternalError {
			t.Fatalf("Error = %q, want fixed message %q (must not echo raw error)", response.Failed[0].Error, msgInternalError)
		}
	})
}

type errWithMessage string

func (e errWithMessage) Error() string { return string(e) }
