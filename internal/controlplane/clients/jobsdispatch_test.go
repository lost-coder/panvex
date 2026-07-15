package clients

import (
	"context"
	"encoding/json"
	"errors"
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

	job, err := svc.EnqueueClientJob(context.Background(), "actor-1", jobs.ActionClientUpdate, client, "old-name", []string{"agent-1", "agent-2"}, observedAt)
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
		PreviousName:      "old-name",
		Name:              "alice",
		Secret:            "supersecret",
		UserADTag:         "adtag",
		Enabled:           true,
		MaxTCPConns:       5,
		MaxUniqueIPs:      3,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-01-01T00:00:00Z",
	}
	if payload != want {
		t.Fatalf("payload = %+v want %+v", payload, want)
	}

	// Exact JSON string, to lock the wire tags byte-for-byte.
	wantJSON := `{"client_id":"client-1","previous_name":"old-name","name":"alice","secret":"supersecret","user_ad_tag":"adtag","enabled":true,"max_tcp_conns":5,"max_unique_ips":3,"data_quota_bytes":1024,"expiration_rfc3339":"2026-01-01T00:00:00Z"}`
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

func TestEnqueueClientJobPreviousNameOmitted(t *testing.T) {
	t.Parallel()

	deps := &fakeDeps{ttl: time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-2"}}
	svc := NewService(ServiceConfig{})
	svc.SetDeps(deps, jq, nil)

	client := Client{ID: ClientID("client-2"), Name: "bob"}
	if _, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientCreate, client, "", nil, time.Now()); err != nil {
		t.Fatalf("EnqueueClientJob: %v", err)
	}
	if got := jq.enqueued[0].PayloadJSON; !jsonHasNoKey(t, got, "previous_name") {
		t.Fatalf("PayloadJSON = %s, want previous_name omitted when empty", got)
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

	err := svc.DispatchClientUpdateJobs(context.Background(), "actor", client, "old-carol", currentDeployments, targetAgentIDs, time.Now())
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
	if err := svc.DispatchClientUpdateJobs(context.Background(), "actor", client, "", nil, nil, time.Now()); err != nil {
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
	_, err := svc.EnqueueClientJob(context.Background(), "actor", jobs.ActionClientCreate, client, "", []string{"agent-1"}, time.Now())
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
