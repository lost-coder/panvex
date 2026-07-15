package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestOfflineNodeConvergesOnDeleteAfterReconnect is the scenario from the
// rollout diagnosis (C1/R-2): a node is offline when an operator deletes a
// client, the delete job expires unanswered, and the node comes back — still
// serving credentials the panel considers revoked.
//
// The reconnect reconcile must re-send the delete from the CURRENT desired
// state, with no operator action.
func TestOfflineNodeConvergesOnDeleteAfterReconnect(t *testing.T) {
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	currentTime := now
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
		Store:            store,
	})
	defer server.Close()

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	client, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient() error = %v", err)
	}

	// The node applies the create, so that deployment is converged.
	createJob := server.jobs.ListWithContext(context.Background())[0]
	server.recordClientJobResultWithContext(context.Background(), "agent-000001", createJob.ID, true, "ok", "{}", now)

	// The operator deletes the client while the node is offline. The delete job
	// is enqueued and never answered.
	deleteAt := now.Add(time.Minute)
	currentTime = deleteAt
	if err := server.clientsSvc.DeleteFlow(context.Background(), string(client.ID), "user-000001", deleteAt); err != nil {
		t.Fatalf("deleteClient() error = %v", err)
	}
	if got := countJobsWithAction(t, server, jobs.ActionClientDelete); got != 1 {
		t.Fatalf("delete jobs after deleteClient = %d, want 1", got)
	}

	// The job's TTL passes with the node still away. Before R10 this was the
	// end of the story: the deployment stayed queued forever and the node kept
	// the client.
	currentTime = deleteAt.Add(clientJobTTL + time.Minute)
	assertDeploymentUnconfirmed(t, server, client.ID, "agent-000001", string(jobs.ActionClientDelete))

	// The node reconnects.
	server.reconcileClientDeployments(context.Background(), "agent-000001")

	if got := countJobsWithAction(t, server, jobs.ActionClientDelete); got != 2 {
		t.Fatalf("delete jobs after reconnect = %d, want 2 (the original plus the re-sent one)", got)
	}
}

// TestReconcileSkipsConfirmedDeployments guards the other direction: a node that
// already applied its desired state must not be handed the same job again on
// every reconnect.
func TestReconcileSkipsConfirmedDeployments(t *testing.T) {
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	currentTime := now
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
		Store:            store,
	})
	defer server.Close()

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	if _, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("createClient() error = %v", err)
	}
	createJob := server.jobs.ListWithContext(context.Background())[0]
	server.recordClientJobResultWithContext(context.Background(), "agent-000001", createJob.ID, true, "ok", "{}", now)

	currentTime = now.Add(2 * clientJobTTL)
	if enqueued := server.reconcileClientDeployments(context.Background(), "agent-000001"); enqueued != 0 {
		t.Fatalf("reconcile enqueued %d jobs for a converged deployment, want 0", enqueued)
	}
}

// TestReconcileThrottlesRepeatedAttempts: a node that stays away must not be
// handed a fresh job on every trigger — the throttle is one re-send per job TTL.
func TestReconcileThrottlesRepeatedAttempts(t *testing.T) {
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	currentTime := now
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
		Store:            store,
	})
	defer server.Close()

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	if _, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("createClient() error = %v", err)
	}

	// Never confirmed. Two reconciles back to back: the second must be a no-op.
	currentTime = now.Add(clientJobTTL + time.Minute)
	if enqueued := server.reconcileClientDeployments(context.Background(), "agent-000001"); enqueued != 1 {
		t.Fatalf("first reconcile enqueued %d jobs, want 1", enqueued)
	}
	if enqueued := server.reconcileClientDeployments(context.Background(), "agent-000001"); enqueued != 0 {
		t.Fatalf("second reconcile enqueued %d jobs, want 0 (throttled within the TTL window)", enqueued)
	}

	// Past the throttle window it tries again.
	currentTime = currentTime.Add(clientJobTTL + time.Minute)
	if enqueued := server.reconcileClientDeployments(context.Background(), "agent-000001"); enqueued != 1 {
		t.Fatalf("reconcile past the throttle window enqueued %d jobs, want 1", enqueued)
	}
}

// TestExpiredClientJobSurfacesAsAwaitingNode is the R10b core (C5): a client
// job that expires before the node confirms it must flip its deployment from
// "queued" to "awaiting_node" — so the operator sees "waiting for the node",
// not an eternal "queued" — while the reconciler still re-sends it, and a later
// success returns it to "succeeded".
func TestExpiredClientJobSurfacesAsAwaitingNode(t *testing.T) {
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	currentTime := now
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
		Store:            store,
	})
	defer server.Close()

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	client, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient() error = %v", err)
	}

	// Fresh create job: the deployment is queued, not yet confirmed.
	deployment := mirrorDeployment(server, string(client.ID), "agent-000001")
	if deployment.Status != clients.DeploymentStatusQueued {
		t.Fatalf("deployment.Status after createClient = %q, want %q", deployment.Status, clients.DeploymentStatusQueued)
	}

	// The node never answers and the create job's TTL elapses. The next sweep
	// seals the job expired and fires the expiry hook, flipping the deployment.
	currentTime = now.Add(clientJobTTL + time.Minute)
	server.jobs.ListWithContext(context.Background())

	deployment = mirrorDeployment(server, string(client.ID), "agent-000001")
	if deployment.Status != clients.DeploymentStatusAwaitingNode {
		t.Fatalf("deployment.Status after expiry = %q, want %q", deployment.Status, clients.DeploymentStatusAwaitingNode)
	}
	if deployment.LastError == "" {
		t.Fatal("deployment.LastError is empty after expiry; want a 'node did not confirm' message")
	}

	// The reconciler treats awaiting_node as the same re-send class as queued.
	if enqueued := server.reconcileClientDeployments(context.Background(), "agent-000001"); enqueued != 1 {
		t.Fatalf("reconcile of an awaiting_node deployment enqueued %d jobs, want 1", enqueued)
	}

	// The node finally applies the re-sent create; the deployment converges.
	resent := latestQueuedJob(t, server, jobs.ActionClientCreate)
	server.recordClientJobResultWithContext(context.Background(), "agent-000001", resent.ID, true, "ok", "{}", currentTime)

	deployment = mirrorDeployment(server, string(client.ID), "agent-000001")
	if deployment.Status != clients.DeploymentStatusSucceeded {
		t.Fatalf("deployment.Status after success = %q, want %q", deployment.Status, clients.DeploymentStatusSucceeded)
	}
	if deployment.LastError != "" {
		t.Fatalf("deployment.LastError after success = %q, want empty", deployment.LastError)
	}
}

// latestQueuedJob returns the most recently created still-queued job for an
// action — the re-sent job after an expiry, distinct from the expired original.
func latestQueuedJob(t *testing.T, server *Server, action jobs.Action) jobs.Job {
	t.Helper()
	var newest jobs.Job
	found := false
	for _, job := range server.jobs.ListWithContext(context.Background()) {
		if job.Action != action || job.Status != jobs.StatusQueued {
			continue
		}
		if !found || job.CreatedAt.After(newest.CreatedAt) {
			newest = job
			found = true
		}
	}
	if !found {
		t.Fatalf("no queued job with action %q", action)
	}
	return newest
}

func countJobsWithAction(t *testing.T, server *Server, action jobs.Action) int {
	t.Helper()
	count := 0
	for _, job := range server.jobs.ListWithContext(context.Background()) {
		if job.Action == action {
			count++
		}
	}
	return count
}

func assertDeploymentUnconfirmed(t *testing.T, server *Server, clientID clients.ClientID, agentID, wantOperation string) {
	t.Helper()
	mirror := server.clientsSvc.MirrorSnapshot()
	deployment, ok := mirror.Deployments[clientID][agentID]
	if !ok {
		t.Fatalf("no deployment row for (%s, %s)", clientID, agentID)
	}
	if deployment.DesiredOperation != wantOperation {
		t.Fatalf("deployment.DesiredOperation = %q, want %q", deployment.DesiredOperation, wantOperation)
	}
	if deployment.Status == clients.DeploymentStatusSucceeded {
		t.Fatal("deployment is marked succeeded; the node never answered")
	}
}

// TestTopologyChangeRollsClientsOntoAndOffTheNode is C4 from the diagnosis:
// moving an agent between fleet groups used to produce no jobs at all, so the
// node kept the old group's clients and never received the new group's.
func TestTopologyChangeRollsClientsOntoAndOffTheNode(t *testing.T) {
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	currentTime := now
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
		Store:            store,
	})
	defer server.Close()

	groupA := seedClientTargetAgent(t, store, server, "group-a", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	groupB := seedClientTargetAgent(t, store, server, "group-b", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000002",
		NodeName:   "node-b",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	// One client per group.
	if _, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupA},
	}, now); err != nil {
		t.Fatalf("createClient(alice) error = %v", err)
	}
	if _, _, _, err := server.clientsSvc.Create(context.Background(), "user-000001", clients.MutationInput{
		Name:          "bob",
		FleetGroupIDs: []string{groupB},
	}, now); err != nil {
		t.Fatalf("createClient(bob) error = %v", err)
	}

	// Move node-a from group A to group B.
	currentTime = now.Add(time.Minute)
	server.mu.Lock()
	server.updateAgentIdentity("agent-000001", func(a *Agent) { a.FleetGroupID = groupB })
	server.mu.Unlock()

	server.reconcileClientTopology(context.Background(), "user-000001", currentTime)

	mirror := server.clientsSvc.MirrorSnapshot()
	alice := findClientByName(t, mirror, "alice")
	bob := findClientByName(t, mirror, "bob")

	// alice (group A) must be pulled off node-a: it is no longer a target.
	if op := mirror.Deployments[alice]["agent-000001"].DesiredOperation; op != string(jobs.ActionClientDelete) {
		t.Fatalf("alice on agent-000001: DesiredOperation = %q, want %q", op, jobs.ActionClientDelete)
	}
	// bob (group B) must be rolled onto node-a: it just became a target.
	if _, ok := mirror.Deployments[bob]["agent-000001"]; !ok {
		t.Fatal("bob has no deployment on agent-000001; joining group B must roll the group's clients onto the node")
	}
}

func findClientByName(t *testing.T, mirror clients.MirrorState, name string) clients.ClientID {
	t.Helper()
	for id, client := range mirror.Clients {
		if client.Name == name {
			return id
		}
	}
	t.Fatalf("no client named %q in the mirror", name)
	return ""
}
