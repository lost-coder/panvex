package clients

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

func TestEnqueueClientJobPayloadAndDispatch(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{
		readOnly: map[string]bool{"agent-1": true},
		ttl:      7 * time.Minute,
	}
	jq := &fakeJobQueue{
		enqueueJob: jobs.Job{ID: "job-1", TargetAgentIDs: []string{"agent-1", "agent-2"}},
	}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{
		ID:                ClientID("client-1"),
		Name:              "alice",
		Secret:            "supersecret",
		UserADTag:         "adtag",
		Enabled:           true,
		MaxTCPConns:       5,
		MaxUniqueIPs:      3,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-01-01T00:00:00Z",
	}
	observedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	job, err := svc.EnqueueClientJob(context.Background(), "actor-1", jobs.ActionClientUpdate, client, []string{"agent-1", "agent-2"}, observedAt)
	if err != nil {
		t.Fatalf("EnqueueClientJob: unexpected error: %v", err)
	}
	if job.ID != "job-1" {
		t.Fatalf("EnqueueClientJob: got job %+v", job)
	}

	if len(jq.enqueued) != 1 {
		t.Fatalf("EnqueueClientJob: got %d enqueue calls want 1", len(jq.enqueued))
	}
	input := jq.enqueued[0]
	if input.Action != jobs.ActionClientUpdate {
		t.Fatalf("EnqueueClientJob: action = %v", input.Action)
	}
	if input.TTL != 7*time.Minute {
		t.Fatalf("EnqueueClientJob: TTL = %v want deps.ClientJobTTL()", input.TTL)
	}
	if input.ActorID != "actor-1" {
		t.Fatalf("EnqueueClientJob: ActorID = %q", input.ActorID)
	}
	if !input.ReadOnlyAgents["agent-1"] {
		t.Fatalf("EnqueueClientJob: ReadOnlyAgents = %+v, want agent-1 read-only from deps.ReadOnlyAgents", input.ReadOnlyAgents)
	}
	if _, ok := input.ReadOnlyAgents["agent-2"]; ok {
		t.Fatalf("EnqueueClientJob: ReadOnlyAgents unexpectedly contains agent-2")
	}

	// Prove the wire shape is intact: unmarshal and field-equality against
	// every Client field the payload is documented to carry.
	var payload ClientJobPayload
	if err := json.Unmarshal([]byte(input.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	want := ClientJobPayload{
		ClientID:          "client-1",
		Name:              "alice",
		Secret:            "supersecret",
		UserADTag:         "adtag",
		Enabled:           true,
		MaxTCPConns:       5,
		MaxUniqueIPs:      3,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-01-01T00:00:00Z",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %+v want %+v", payload, want)
	}

	// Exact JSON string, to lock the wire tags byte-for-byte. previous_name
	// was DELIBERATELY removed (audit F2: Telemt has no rename operation).
	wantJSON := `{"client_id":"client-1","name":"alice","secret":"supersecret","user_ad_tag":"adtag","enabled":true,"max_tcp_conns":5,"max_unique_ips":3,"data_quota_bytes":1024,"expiration_rfc3339":"2026-01-01T00:00:00Z"}`
	if input.PayloadJSON != wantJSON {
		t.Fatalf("PayloadJSON = %s want %s", input.PayloadJSON, wantJSON)
	}

	if len(deps.notified) != 1 || len(deps.notified[0]) != 2 {
		t.Fatalf("NotifyAgentSessions: got %+v want one call with 2 target agents", deps.notified)
	}
	if len(deps.publishedJobs) != 1 || deps.publishedJobs[0].ID != "job-1" {
		t.Fatalf("PublishJobCreated: got %+v", deps.publishedJobs)
	}
}

// TestEnqueueClientJobHasNoPreviousName pins the F2 removal: the rename
// plumbing is gone, so no client job payload may carry a previous_name key.
func TestEnqueueClientJobHasNoPreviousName(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-2"}}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-2"), Name: "bob"}
	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientCreate, client, nil, time.Now()); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}
	if got := jq.enqueued[0].PayloadJSON; !jsonHasNoKey(t, got, "previous_name") {
		t.Fatalf("PayloadJSON = %s, want no previous_name key (rename removed, audit F2)", got)
	}
}

// nameOwnerTestService seeds a mirror in which "alice" was client-old's name,
// client-old is tombstoned and still deployed to agent-1 and agent-2, and a new
// client re-used the freed name — deployed to agent-1 ONLY.
//
// That asymmetry is the whole point: the Telemt user "alice" on agent-2 is
// still client-old's, so the delete must reach agent-2 even though the name is
// live elsewhere.
func nameOwnerTestService(t *testing.T, jq *fakeJobQueue, deletedAt time.Time, newOwnerAgents ...string) (*Service, Client) {
	t.Helper()

	svc := NewService(ServiceConfig{})
	svc.SetDeps(&fakeDeps{ttl: time.Minute}, jq, nil)

	old := Client{ID: ClientID("client-old"), Name: "alice", Secret: "s-old", DeletedAt: &deletedAt}
	svc.MirrorReplaceInMemory(old, nil, []Deployment{
		{ClientID: old.ID, AgentID: "agent-1", DesiredOperation: string(jobs.ActionClientDelete), Status: DeploymentStatusQueued},
		{ClientID: old.ID, AgentID: "agent-2", DesiredOperation: string(jobs.ActionClientDelete), Status: DeploymentStatusQueued},
	})

	if len(newOwnerAgents) > 0 {
		deployments := make([]Deployment, 0, len(newOwnerAgents))
		for _, agentID := range newOwnerAgents {
			deployments = append(deployments, Deployment{
				ClientID:         ClientID("client-new"),
				AgentID:          agentID,
				DesiredOperation: string(jobs.ActionClientUpdate),
				Status:           DeploymentStatusQueued,
			})
		}
		svc.MirrorReplaceInMemory(Client{ID: ClientID("client-new"), Name: "alice", Secret: "s-new"}, nil, deployments)
	}
	return svc, old
}

// TestEnqueueClientDeleteJobCarriesPerAgentNameOwner is the core of the panel
// half: the delete payload must name the client that currently owns the name ON
// EACH TARGET AGENT, and must stay silent about agents where nobody else does.
// One delete job fans out to many agents with one shared payload, so a scalar
// could not express this — hence the map.
func TestEnqueueClientDeleteJobCarriesPerAgentNameOwner(t *testing.T) {
	t.Parallel()

	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-del"}}
	deletedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc, old := nameOwnerTestService(t, jq, deletedAt, "agent-1")

	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientDelete, old, []string{"agent-1", "agent-2"}, deletedAt); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}

	payload := decodeClientJobPayload(t, jq.enqueued[0].PayloadJSON)
	want := map[string]string{"agent-1": "client-new"}
	if !reflect.DeepEqual(payload.NameOwnerByAgent, want) {
		t.Fatalf("NameOwnerByAgent = %+v, want %+v (agent-2 has no other owner and must be absent)", payload.NameOwnerByAgent, want)
	}
}

// TestEnqueueClientDeleteJobOmitsNameOwnerWhenNameNotReused pins the common
// case: nobody re-used the name, so the signal is absent entirely and the agent
// deletes. The key must not appear at all — an empty map would be equivalent to
// the agent, but omitempty keeps the wire form of the overwhelmingly common
// case byte-identical to before.
func TestEnqueueClientDeleteJobOmitsNameOwnerWhenNameNotReused(t *testing.T) {
	t.Parallel()

	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-del"}}
	deletedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc, old := nameOwnerTestService(t, jq, deletedAt)

	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientDelete, old, []string{"agent-1", "agent-2"}, deletedAt); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}

	got := jq.enqueued[0].PayloadJSON
	if !jsonHasNoKey(t, got, "name_owner_by_agent") {
		t.Fatalf("PayloadJSON = %s, want no name_owner_by_agent key when the name was never re-used", got)
	}
}

// TestEnqueueClientDeleteJobIgnoresTombstonedAndSelfOwners proves the two
// exclusions the lookup must make: the client being deleted never counts as
// "another owner" of its own name, and neither does a THIRD tombstoned client
// that once held it (its Telemt user is itself being torn down).
func TestEnqueueClientDeleteJobIgnoresTombstonedAndSelfOwners(t *testing.T) {
	t.Parallel()

	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-del"}}
	deletedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc, old := nameOwnerTestService(t, jq, deletedAt)

	// A third client that also held "alice" and is likewise tombstoned.
	svc.MirrorReplaceInMemory(
		Client{ID: ClientID("client-older"), Name: "alice", DeletedAt: &deletedAt},
		nil,
		[]Deployment{{ClientID: ClientID("client-older"), AgentID: "agent-1", DesiredOperation: string(jobs.ActionClientDelete)}},
	)

	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientDelete, old, []string{"agent-1", "agent-2"}, deletedAt); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}

	payload := decodeClientJobPayload(t, jq.enqueued[0].PayloadJSON)
	if len(payload.NameOwnerByAgent) != 0 {
		t.Fatalf("NameOwnerByAgent = %+v, want empty (self and tombstoned clients are not owners)", payload.NameOwnerByAgent)
	}
}

// TestEnqueueClientJobOmitsNameOwnerForNonDeleteActions keeps the signal scoped
// to the one action that reads it. A create/update/rotate payload carrying it
// would be dead weight on the wire and an invitation to misuse.
func TestEnqueueClientJobOmitsNameOwnerForNonDeleteActions(t *testing.T) {
	t.Parallel()

	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-upd"}}
	deletedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc, old := nameOwnerTestService(t, jq, deletedAt, "agent-1")

	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientUpdate, old, []string{"agent-1"}, deletedAt); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}
	if got := jq.enqueued[0].PayloadJSON; !jsonHasNoKey(t, got, "name_owner_by_agent") {
		t.Fatalf("PayloadJSON = %s, want no name_owner_by_agent key on a non-delete action", got)
	}
}

// TestReconcileDeploymentsRecomputesNameOwnerOnReEnqueue is the path that
// matters most: the re-enqueue that gives an expired delete a fresh CreatedAt
// is exactly when the name may have been re-used since the original job was
// built. The signal must be recomputed from CURRENT mirror state at every
// enqueue, never carried over from the first one.
func TestReconcileDeploymentsRecomputesNameOwnerOnReEnqueue(t *testing.T) {
	t.Parallel()

	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-del"}}
	deletedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc, _ := nameOwnerTestService(t, jq, deletedAt)

	// First pass: the name is still free, so the delete carries no owner.
	first := deletedAt
	svc.SetNow(func() time.Time { return first })
	if enqueued := svc.ReconcileDeployments(context.Background(), ""); enqueued != 1 {
		t.Fatalf("first ReconcileDeployments() = %d jobs, want 1", enqueued)
	}
	if got := decodeClientJobPayload(t, jq.enqueued[0].PayloadJSON); len(got.NameOwnerByAgent) != 0 {
		t.Fatalf("first re-enqueue NameOwnerByAgent = %+v, want empty", got.NameOwnerByAgent)
	}

	// An operator now re-uses the freed name on agent-1 only.
	svc.MirrorReplaceInMemory(
		Client{ID: ClientID("client-new"), Name: "alice", Secret: "s-new"},
		nil,
		[]Deployment{{ClientID: ClientID("client-new"), AgentID: "agent-1", DesiredOperation: string(jobs.ActionClientUpdate), Status: DeploymentStatusQueued}},
	)

	// Second pass, past the per-pair throttle: the payload must now name the
	// new owner on agent-1 — and still leave agent-2 free to delete.
	second := first.Add(clientReconcileMinInterval + time.Minute)
	svc.SetNow(func() time.Time { return second })
	if enqueued := svc.ReconcileDeployments(context.Background(), ""); enqueued == 0 {
		t.Fatal("second ReconcileDeployments() enqueued nothing, want a re-send past the throttle")
	}

	// The pass also re-sends client-new's own (unconfirmed) update, and mirror
	// map order decides which lands first — pick the delete explicitly.
	var resent *ClientJobPayload
	for _, input := range jq.enqueued[1:] {
		if input.Action != jobs.ActionClientDelete {
			continue
		}
		payload := decodeClientJobPayload(t, input.PayloadJSON)
		if payload.ClientID == "client-old" {
			resent = &payload
		}
	}
	if resent == nil {
		t.Fatal("second pass enqueued no client.delete for client-old")
	}

	want := map[string]string{"agent-1": "client-new"}
	if !reflect.DeepEqual(resent.NameOwnerByAgent, want) {
		t.Fatalf("re-enqueued NameOwnerByAgent = %+v, want %+v (recomputed, not copied from the first enqueue)", resent.NameOwnerByAgent, want)
	}
}

func jsonHasNoKey(t *testing.T, payload, key string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	_, ok := m[key]
	return !ok
}

func TestDispatchClientUpdateJobs(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-x"}}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-3"), Name: "carol"}
	currentDeployments := []Deployment{
		{ClientID: ClientID("client-3"), AgentID: "agent-a"},
		{ClientID: ClientID("client-3"), AgentID: "agent-b"},
	}
	targetAgentIDs := []string{"agent-a"} // agent-b dropped

	err := svc.DispatchClientUpdateJobs(context.Background(), "actor", client, currentDeployments, targetAgentIDs, time.Now())
	if err != nil {
		t.Fatalf("DispatchClientUpdateJobs: %v", err)
	}

	if len(jq.enqueued) != 2 {
		t.Fatalf("DispatchClientUpdateJobs: got %d enqueue calls want 2 (update + delete)", len(jq.enqueued))
	}
	updateInput := jq.enqueued[0]
	if updateInput.Action != jobs.ActionClientUpdate {
		t.Fatalf("first enqueue: action = %v want ActionClientUpdate", updateInput.Action)
	}
	if len(updateInput.TargetAgentIDs) != 1 || updateInput.TargetAgentIDs[0] != "agent-a" {
		t.Fatalf("first enqueue: TargetAgentIDs = %v want [agent-a]", updateInput.TargetAgentIDs)
	}
	deleteInput := jq.enqueued[1]
	if deleteInput.Action != jobs.ActionClientDelete {
		t.Fatalf("second enqueue: action = %v want ActionClientDelete", deleteInput.Action)
	}
	if len(deleteInput.TargetAgentIDs) != 1 || deleteInput.TargetAgentIDs[0] != "agent-b" {
		t.Fatalf("second enqueue: TargetAgentIDs = %v want [agent-b] (removed target)", deleteInput.TargetAgentIDs)
	}
}

func TestDispatchClientUpdateJobsNoTargetsNoRemovals(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-y"}}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-4")}
	if err := svc.DispatchClientUpdateJobs(context.Background(), "actor", client, nil, nil, time.Now()); err != nil {
		t.Fatalf("DispatchClientUpdateJobs: %v", err)
	}
	if len(jq.enqueued) != 0 {
		t.Fatalf("DispatchClientUpdateJobs: got %d enqueue calls want 0", len(jq.enqueued))
	}
}

func TestEnqueueClientResetQuotaJobPayloadOnlyIDAndName(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: 2 * time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-reset", TargetAgentIDs: []string{"agent-1"}}}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{
		ID:                ClientID("client-5"),
		Name:              "dave",
		Secret:            "topsecret",
		MaxTCPConns:       10,
		MaxUniqueIPs:      10,
		DataQuotaBytes:    999,
		ExpirationRFC3339: "2026-06-01T00:00:00Z",
	}

	job, err := svc.EnqueueClientResetQuotaJob(context.Background(), "actor", client, []string{"agent-1"}, time.Now())
	if err != nil {
		t.Fatalf("EnqueueClientResetQuotaJob: %v", err)
	}
	if job.ID != "job-reset" {
		t.Fatalf("job = %+v", job)
	}

	payload := jq.enqueued[0].PayloadJSON
	wantJSON := `{"client_id":"client-5","name":"dave"}`
	if payload != wantJSON {
		t.Fatalf("PayloadJSON = %s want %s (client_id + name only, no secret/limits)", payload, wantJSON)
	}
	if jsonHasKey(t, payload, "secret") {
		t.Fatalf("PayloadJSON leaks secret: %s", payload)
	}
	if len(deps.notified) != 1 {
		t.Fatalf("NotifyAgentSessions: got %d calls want 1", len(deps.notified))
	}
}

func jsonHasKey(t *testing.T, payload, key string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	_, ok := m[key]
	return ok
}

func TestEnqueueClientJobQueueErrorPropagatesNoNotify(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	wantErr := errors.New("enqueue failed")
	jq := &fakeJobQueue{enqueueErr: wantErr}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-6"), Name: "eve"}
	_, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientCreate, client, []string{"agent-1"}, time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnqueueClientJob: err = %v want %v", err, wantErr)
	}
	if len(deps.notified) != 0 {
		t.Fatalf("NotifyAgentSessions: got %d calls want 0 on enqueue error", len(deps.notified))
	}
	if len(deps.publishedJobs) != 0 {
		t.Fatalf("PublishJobCreated: got %d calls want 0 on enqueue error", len(deps.publishedJobs))
	}
}

func TestEnqueueClientResetQuotaJobQueueErrorPropagatesNoNotify(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	wantErr := errors.New("enqueue failed")
	jq := &fakeJobQueue{enqueueErr: wantErr}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-7"), Name: "frank"}
	_, err := svc.EnqueueClientResetQuotaJob(context.Background(), "actor", client, []string{"agent-1"}, time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnqueueClientResetQuotaJob: err = %v want %v", err, wantErr)
	}
	if len(deps.notified) != 0 {
		t.Fatalf("NotifyAgentSessions: got %d calls want 0 on enqueue error", len(deps.notified))
	}
}
