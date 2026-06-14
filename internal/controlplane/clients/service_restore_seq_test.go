package clients

import (
	"context"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
)

// TestRestoreRecoversIDSequences verifies that Service.Restore seeds all
// three monotonic counters (client, assignment, discovered) from the
// persisted records, so the next NextClientID / NextAssignmentID /
// NextDiscoveredID call returns a value strictly greater than any
// previously-stored ID — without a separate RecoverSequencesFromRecords
// call.
func TestRestoreRecoversIDSequences(t *testing.T) {
	t.Parallel()

	// Seed the fake clients repo with one client + one assignment.
	repo := newFakeRepo()
	clientID := ClientID("client-0000007")
	repo.clientsByID[clientID] = Client{ID: clientID, Secret: ""}
	repo.assignmentsByClient[clientID] = []Assignment{
		{ID: AssignmentID("client-assignment-0000100"), ClientID: clientID},
	}

	// Seed the fake discovered repo with one record.
	discRepo := newFakeDiscoveredRepo()
	discID := discovered.DiscoveredID("discovered-0000050")
	discRepo.byID[discID] = discovered.DiscoveredClient{ID: discID}

	rs := &fakeRepoSet{clients: repo, discovered: discRepo}
	svc := NewService(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: discRepo,
		UoW:            newFakeUoW(rs),
	})

	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// After Restore, the next allocated ID must exceed the persisted max.
	// client sequence was at 7  → next must be client-0000008
	if got := svc.NextClientID(); got != "client-0000008" {
		t.Errorf("NextClientID after Restore = %q; want client-0000008", got)
	}
	// assignment sequence was at 100 → next must be client-assignment-0000101
	if got := svc.NextAssignmentID(); got != "client-assignment-0000101" {
		t.Errorf("NextAssignmentID after Restore = %q; want client-assignment-0000101", got)
	}
	// discovered sequence was at 50 → next must be discovered-0000051
	if got := svc.NextDiscoveredID(); got != "discovered-0000051" {
		t.Errorf("NextDiscoveredID after Restore = %q; want discovered-0000051", got)
	}
}
