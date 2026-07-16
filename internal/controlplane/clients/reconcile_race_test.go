package clients

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// The R10-1 / R10-2 interleavings: both reconcilers compute from one
// MirrorSnapshot and then write (a job payload, a full client row) built from
// that snapshot. An operator mutation that commits BETWEEN the snapshot and
// the write used to be silently reverted — and because the reconciler's job
// is created later, supersede-by-client expired the operator's fresher job,
// so nothing ever re-sent the newer state (a defeated revocation reported as
// success). These tests construct the interleaving deterministically by
// hooking the deps calls the passes make between snapshot and write.

// hookedDeps wraps fakeDeps with call hooks so a test can mutate the mirror
// at a precise point inside a reconcile pass — standing in for a concurrent
// operator request.
type hookedDeps struct {
	*fakeDeps
	onReadOnlyAgents func(agentIDs []string)
	onTopology       func()
}

func (h *hookedDeps) ReadOnlyAgents(agentIDs []string) map[string]bool {
	if h.onReadOnlyAgents != nil {
		h.onReadOnlyAgents(agentIDs)
	}
	return h.fakeDeps.ReadOnlyAgents(agentIDs)
}

func (h *hookedDeps) Topology() AgentTopology {
	if h.onTopology != nil {
		h.onTopology()
	}
	return h.fakeDeps.Topology()
}

// decodeClientJobPayload unmarshals a captured enqueue input.
func decodeClientJobPayload(t *testing.T, payloadJSON string) ClientJobPayload {
	t.Helper()
	var payload ClientJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

// TestReconcileDeploymentsEnqueuesFreshPayloadAfterConcurrentRotate is the
// R10-1 interleaving:
//
//	t1  a reconcile pass snapshots the mirror (client at secret v1),
//	t2  an operator rotate commits secret v2 to the mirror,
//	t3  the pass reaches the client and enqueues its re-send job.
//
// The enqueued payload must carry v2: it is created AFTER the operator's
// rotate job, so supersede-by-client makes IT the job the node applies — a v1
// payload here converges the node onto the revoked secret with
// Status=succeeded, and nothing ever re-sends v2.
//
// Two clients on distinct agents make the interleaving deterministic without
// relying on map order: whichever client the pass enqueues FIRST, the hook
// rotates the OTHER one, and the assertion checks the other one's payload.
func TestReconcileDeploymentsEnqueuesFreshPayloadAfterConcurrentRotate(t *testing.T) {
	t.Parallel()

	deps := &hookedDeps{fakeDeps: &fakeDeps{ttl: time.Minute}}
	jq := &fakeJobQueue{}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	agentFor := map[ClientID]string{
		ClientID("client-a"): "agent-a",
		ClientID("client-b"): "agent-b",
	}
	clientFor := map[string]ClientID{"agent-a": "client-a", "agent-b": "client-b"}
	seed := func(id ClientID) {
		client := Client{ID: id, Name: string(id), Secret: "secret-old-" + string(id), Enabled: true}
		deployment := Deployment{
			ClientID:         id,
			AgentID:          agentFor[id],
			DesiredOperation: string(jobs.ActionClientUpdate),
			Status:           DeploymentStatusQueued,
			UpdatedAt:        observedAt,
		}
		svc.MirrorReplaceInMemory(client, nil, []Deployment{deployment})
	}
	seed("client-a")
	seed("client-b")

	// The concurrent operator rotate, injected between the pass's snapshot
	// and the second enqueue: when the FIRST client's job is being enqueued,
	// rotate the OTHER client's secret in the mirror.
	var rotatedID ClientID
	var once sync.Once
	deps.onReadOnlyAgents = func(agentIDs []string) {
		once.Do(func() {
			if len(agentIDs) != 1 {
				t.Errorf("ReadOnlyAgents called with %v, want a single agent", agentIDs)
				return
			}
			enqueueing := clientFor[agentIDs[0]]
			other := ClientID("client-a")
			if enqueueing == other {
				other = "client-b"
			}
			rotatedID = other
			rotated := Client{ID: other, Name: string(other), Secret: "secret-rotated", Enabled: true}
			deployment := Deployment{
				ClientID:         other,
				AgentID:          agentFor[other],
				DesiredOperation: string(jobs.ActionClientUpdate),
				Status:           DeploymentStatusQueued,
				UpdatedAt:        observedAt,
			}
			svc.MirrorReplaceInMemory(rotated, nil, []Deployment{deployment})
		})
	}

	if enqueued := svc.ReconcileDeployments(context.Background(), ""); enqueued != 2 {
		t.Fatalf("ReconcileDeployments() = %d jobs, want 2", enqueued)
	}
	if len(jq.enqueued) != 2 {
		t.Fatalf("captured %d enqueues, want 2", len(jq.enqueued))
	}

	// The job for the client rotated mid-pass is the SECOND one enqueued; its
	// payload must carry the post-rotate secret.
	second := decodeClientJobPayload(t, jq.enqueued[1].PayloadJSON)
	if second.ClientID != string(rotatedID) {
		t.Fatalf("second enqueue is for %q, want the mid-pass-rotated client %q", second.ClientID, rotatedID)
	}
	if second.Secret != "secret-rotated" {
		t.Fatalf("re-enqueued payload Secret = %q, want the freshly rotated %q (stale snapshot payload would silently undo the rotation)",
			second.Secret, "secret-rotated")
	}
}

// TestReconcileDeploymentsSkipsClientTombstonedMidPass: when the client is
// tombstoned between the snapshot and the enqueue, the pass must NOT ship the
// snapshot's stale update — the delete flow already enqueued the delete job,
// and a later-created update would supersede it (resurrecting the client on
// the node). The next pass re-sends the delete if it goes unconfirmed.
func TestReconcileDeploymentsSkipsClientTombstonedMidPass(t *testing.T) {
	t.Parallel()

	deps := &hookedDeps{fakeDeps: &fakeDeps{ttl: time.Minute}}
	jq := &fakeJobQueue{}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	agentFor := map[ClientID]string{
		ClientID("client-a"): "agent-a",
		ClientID("client-b"): "agent-b",
	}
	clientFor := map[string]ClientID{"agent-a": "client-a", "agent-b": "client-b"}
	seed := func(id ClientID) {
		client := Client{ID: id, Name: string(id), Secret: "secret-" + string(id), Enabled: true}
		deployment := Deployment{
			ClientID:         id,
			AgentID:          agentFor[id],
			DesiredOperation: string(jobs.ActionClientUpdate),
			Status:           DeploymentStatusQueued,
			UpdatedAt:        observedAt,
		}
		svc.MirrorReplaceInMemory(client, nil, []Deployment{deployment})
	}
	seed("client-a")
	seed("client-b")

	var tombstonedID ClientID
	var once sync.Once
	deps.onReadOnlyAgents = func(agentIDs []string) {
		once.Do(func() {
			if len(agentIDs) != 1 {
				t.Errorf("ReadOnlyAgents called with %v, want a single agent", agentIDs)
				return
			}
			enqueueing := clientFor[agentIDs[0]]
			other := ClientID("client-a")
			if enqueueing == other {
				other = "client-b"
			}
			tombstonedID = other
			deletedAt := observedAt
			tombstoned := Client{ID: other, Name: string(other), Secret: "secret-" + string(other), DeletedAt: &deletedAt}
			deployment := Deployment{
				ClientID:         other,
				AgentID:          agentFor[other],
				DesiredOperation: string(jobs.ActionClientDelete),
				Status:           DeploymentStatusQueued,
				UpdatedAt:        observedAt,
			}
			svc.MirrorReplaceInMemory(tombstoned, nil, []Deployment{deployment})
		})
	}

	if enqueued := svc.ReconcileDeployments(context.Background(), ""); enqueued != 1 {
		t.Fatalf("ReconcileDeployments() = %d jobs, want 1 (the mid-pass-tombstoned client must be skipped)", enqueued)
	}
	first := decodeClientJobPayload(t, jq.enqueued[0].PayloadJSON)
	if first.ClientID == string(tombstonedID) {
		t.Fatalf("the enqueued job targets the tombstoned client %q — it must be for the other one", tombstonedID)
	}
}
