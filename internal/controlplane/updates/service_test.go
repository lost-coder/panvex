package updates

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// memStore is an in-memory SettingsStore: five independent nil-able blobs.
type memStore struct {
	settings      json.RawMessage
	state         json.RawMessage
	selfUpdate    json.RawMessage
	pending       json.RawMessage
	pendingTelemt json.RawMessage
}

func (m *memStore) GetUpdateSettings(context.Context) (json.RawMessage, error) {
	return m.settings, nil
}
func (m *memStore) PutUpdateSettings(_ context.Context, b json.RawMessage) error {
	m.settings = b
	return nil
}
func (m *memStore) GetUpdateState(context.Context) (json.RawMessage, error) { return m.state, nil }
func (m *memStore) PutUpdateState(_ context.Context, b json.RawMessage) error {
	m.state = b
	return nil
}
func (m *memStore) GetPanelSelfUpdate(context.Context) (json.RawMessage, error) {
	return m.selfUpdate, nil
}
func (m *memStore) PutPanelSelfUpdate(_ context.Context, b json.RawMessage) error {
	m.selfUpdate = b
	return nil
}
func (m *memStore) GetPendingAgentUpdates(context.Context) (json.RawMessage, error) {
	return m.pending, nil
}
func (m *memStore) PutPendingAgentUpdates(_ context.Context, b json.RawMessage) error {
	m.pending = b
	return nil
}
func (m *memStore) GetPendingTelemtUpdates(context.Context) (json.RawMessage, error) {
	return m.pendingTelemt, nil
}
func (m *memStore) PutPendingTelemtUpdates(_ context.Context, b json.RawMessage) error {
	m.pendingTelemt = b
	return nil
}

func TestPendingAgentUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if _, ok, err := svc.PendingAgentUpdate(ctx, "agent-1"); err != nil || ok {
		t.Fatalf("empty store must have no pending update: ok=%v err=%v", ok, err)
	}
	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	version, ok, err := svc.PendingAgentUpdate(ctx, "agent-1")
	if err != nil || !ok || version != "1.4.0" {
		t.Fatalf("want 1.4.0/true, got %q/%v/%v", version, ok, err)
	}
	if err := svc.ClearPendingAgentUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingAgentUpdate: %v", err)
	}
	if _, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); ok {
		t.Fatal("cleared entry must be gone")
	}
}

// A newer operator click must overwrite the older target rather than append,
// and one agent's pending entry must never disturb another's.
func TestPendingAgentUpdateOverwritesPerAgent(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	for _, version := range []string{"1.4.0", "1.5.0"} {
		if err := svc.SetPendingAgentUpdate(ctx, "agent-1", version); err != nil {
			t.Fatalf("SetPendingAgentUpdate(%s): %v", version, err)
		}
	}
	if err := svc.SetPendingAgentUpdate(ctx, "agent-2", "1.3.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate(agent-2): %v", err)
	}
	if got, _, _ := svc.PendingAgentUpdate(ctx, "agent-1"); got != "1.5.0" {
		t.Fatalf("newer click must win, got %q", got)
	}

	if err := svc.ClearPendingAgentUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingAgentUpdate: %v", err)
	}
	if got, ok, _ := svc.PendingAgentUpdate(ctx, "agent-2"); !ok || got != "1.3.0" {
		t.Fatalf("clearing agent-1 must not touch agent-2, got %q/%v", got, ok)
	}
}

// A pending target the agent can never reach (built without version ldflags,
// a 404 release asset, a bad checksum) must not be retried forever: after
// MaxPendingAgentUpdateFailures failed deliveries the panel gives up and drops
// the desired state, so the reconciler stops re-enqueueing a doomed job on
// every reconnect.
func TestRecordPendingAgentUpdateFailureGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}

	for attempt := 1; attempt < MaxPendingAgentUpdateFailures; attempt++ {
		failures, gaveUp, err := svc.RecordPendingAgentUpdateFailure(ctx, "agent-1", "1.4.0")
		if err != nil {
			t.Fatalf("RecordPendingAgentUpdateFailure(%d): %v", attempt, err)
		}
		if gaveUp {
			t.Fatalf("gave up after %d failures, want it to hold until %d", attempt, MaxPendingAgentUpdateFailures)
		}
		if failures != attempt {
			t.Fatalf("failures = %d, want %d", failures, attempt)
		}
		if _, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); !ok {
			t.Fatalf("target must survive failure %d", attempt)
		}
	}

	failures, gaveUp, err := svc.RecordPendingAgentUpdateFailure(ctx, "agent-1", "1.4.0")
	if err != nil {
		t.Fatalf("final RecordPendingAgentUpdateFailure: %v", err)
	}
	if !gaveUp || failures != MaxPendingAgentUpdateFailures {
		t.Fatalf("failures = %d, gaveUp = %v; want %d/true", failures, gaveUp, MaxPendingAgentUpdateFailures)
	}
	if _, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); ok {
		t.Fatal("giving up must drop the pending target")
	}
}

// A fresh operator click is a new decision: it resets the failure budget, so a
// target that failed before gets its full set of attempts again.
func TestSetPendingAgentUpdateResetsFailureCount(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	for range MaxPendingAgentUpdateFailures - 1 {
		if _, _, err := svc.RecordPendingAgentUpdateFailure(ctx, "agent-1", "1.4.0"); err != nil {
			t.Fatalf("RecordPendingAgentUpdateFailure: %v", err)
		}
	}
	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.5.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate (new click): %v", err)
	}

	failures, gaveUp, err := svc.RecordPendingAgentUpdateFailure(ctx, "agent-1", "1.5.0")
	if err != nil {
		t.Fatalf("RecordPendingAgentUpdateFailure after new click: %v", err)
	}
	if gaveUp || failures != 1 {
		t.Fatalf("a new click must restart the budget, got failures=%d gaveUp=%v", failures, gaveUp)
	}
}

// A failure reported for a version that is no longer the pending target is
// stale (the operator re-clicked while the old job was in flight) and must not
// consume the current target's budget.
func TestRecordPendingAgentUpdateFailureIgnoresStaleVersion(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.5.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	failures, gaveUp, err := svc.RecordPendingAgentUpdateFailure(ctx, "agent-1", "1.4.0")
	if err != nil || gaveUp || failures != 0 {
		t.Fatalf("stale failure must be ignored, got failures=%d gaveUp=%v err=%v", failures, gaveUp, err)
	}
	if version, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); !ok || version != "1.5.0" {
		t.Fatalf("current target must be untouched, got %q/%v", version, ok)
	}
}

// A failure for an agent with nothing pending is a no-op — the update may have
// been dispatched before the panel started tracking desired state.
func TestRecordPendingAgentUpdateFailureWithoutPendingIsNoOp(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	svc := NewService(store)

	failures, gaveUp, err := svc.RecordPendingAgentUpdateFailure(context.Background(), "agent-x", "1.4.0")
	if err != nil || gaveUp || failures != 0 {
		t.Fatalf("no-op expected, got failures=%d gaveUp=%v err=%v", failures, gaveUp, err)
	}
	if store.pending != nil {
		t.Fatalf("a no-op failure must not write a blob, got %s", store.pending)
	}
}

// Clearing an agent that was never pending is a no-op, not an error: the
// reconcile path calls Clear whenever the reported version already matches.
func TestClearPendingAgentUpdateAbsentIsNoOp(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	svc := NewService(store)
	if err := svc.ClearPendingAgentUpdate(context.Background(), "agent-unknown"); err != nil {
		t.Fatalf("clearing an absent entry must not error: %v", err)
	}
	if store.pending != nil {
		t.Fatalf("a no-op clear must not write a blob, got %s", store.pending)
	}
}

// Task 11: telemt.update's pending-desired-state map mirrors agent
// self-update's exactly (same generic accessor underneath, different
// update_config key) — set/get/clear round trip, failures give up on the
// Nth attempt, and a new click resets the budget.

func TestPendingTelemtUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if _, ok, err := svc.PendingTelemtUpdate(ctx, "agent-1"); err != nil || ok {
		t.Fatalf("empty store must have no pending update: ok=%v err=%v", ok, err)
	}
	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}
	version, ok, err := svc.PendingTelemtUpdate(ctx, "agent-1")
	if err != nil || !ok || version != "3.4.25" {
		t.Fatalf("want 3.4.25/true, got %q/%v/%v", version, ok, err)
	}
	if err := svc.ClearPendingTelemtUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingTelemtUpdate: %v", err)
	}
	if _, ok, _ := svc.PendingTelemtUpdate(ctx, "agent-1"); ok {
		t.Fatal("cleared entry must be gone")
	}
}

// TestClearPendingTelemtUpdateIfVersionKeepsNewerTarget: a success result for
// a stale/superseded job (version A) must not clear a newer pending target
// (version B) set by a later operator click while the stale job was still
// in flight. A matching version, on the other hand, must clear.
func TestClearPendingTelemtUpdateIfVersionKeepsNewerTarget(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.26"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}

	// A stale success report for the superseded version A must be a no-op.
	if err := svc.ClearPendingTelemtUpdateIfVersion(ctx, "agent-1", "3.4.25"); err != nil {
		t.Fatalf("ClearPendingTelemtUpdateIfVersion(stale): %v", err)
	}
	version, ok, err := svc.PendingTelemtUpdate(ctx, "agent-1")
	if err != nil || !ok || version != "3.4.26" {
		t.Fatalf("stale clear must not touch the newer pending target, got %q/%v/%v", version, ok, err)
	}

	// A success report for the CURRENT pending version clears it.
	if err := svc.ClearPendingTelemtUpdateIfVersion(ctx, "agent-1", "3.4.26"); err != nil {
		t.Fatalf("ClearPendingTelemtUpdateIfVersion(current): %v", err)
	}
	if _, ok, _ := svc.PendingTelemtUpdate(ctx, "agent-1"); ok {
		t.Fatal("clearing the current pending version must remove it")
	}
}

// The telemt.update pending map is independent of the agent self-update
// pending map: writing one must never disturb the other, even for the same
// agent ID.
func TestPendingTelemtUpdateIndependentOfAgentUpdate(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}

	if got, _, _ := svc.PendingAgentUpdate(ctx, "agent-1"); got != "1.4.0" {
		t.Fatalf("agent pending = %q, want 1.4.0 (telemt write must not disturb it)", got)
	}
	if got, _, _ := svc.PendingTelemtUpdate(ctx, "agent-1"); got != "3.4.25" {
		t.Fatalf("telemt pending = %q, want 3.4.25", got)
	}

	if err := svc.ClearPendingTelemtUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingTelemtUpdate: %v", err)
	}
	if got, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); !ok || got != "1.4.0" {
		t.Fatalf("clearing telemt pending must not touch agent pending, got %q/%v", got, ok)
	}
}

// A pending Telemt target the node can never reach must not be retried
// forever: after MaxPendingAgentUpdateFailures failed deliveries the panel
// gives up and drops the desired state (same budget as agent self-update).
func TestRecordPendingTelemtUpdateFailureGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}

	for attempt := 1; attempt < MaxPendingAgentUpdateFailures; attempt++ {
		failures, gaveUp, err := svc.RecordPendingTelemtUpdateFailure(ctx, "agent-1", "3.4.25")
		if err != nil {
			t.Fatalf("RecordPendingTelemtUpdateFailure(%d): %v", attempt, err)
		}
		if gaveUp {
			t.Fatalf("gave up after %d failures, want it to hold until %d", attempt, MaxPendingAgentUpdateFailures)
		}
		if failures != attempt {
			t.Fatalf("failures = %d, want %d", failures, attempt)
		}
		if _, ok, _ := svc.PendingTelemtUpdate(ctx, "agent-1"); !ok {
			t.Fatalf("target must survive failure %d", attempt)
		}
	}

	failures, gaveUp, err := svc.RecordPendingTelemtUpdateFailure(ctx, "agent-1", "3.4.25")
	if err != nil {
		t.Fatalf("final RecordPendingTelemtUpdateFailure: %v", err)
	}
	if !gaveUp || failures != MaxPendingAgentUpdateFailures {
		t.Fatalf("failures = %d, gaveUp = %v; want %d/true", failures, gaveUp, MaxPendingAgentUpdateFailures)
	}
	if _, ok, _ := svc.PendingTelemtUpdate(ctx, "agent-1"); ok {
		t.Fatal("giving up must drop the pending target")
	}
}

// A fresh operator click resets the telemt.update failure budget, same as
// agent self-update.
func TestSetPendingTelemtUpdateResetsFailureCount(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}
	for range MaxPendingAgentUpdateFailures - 1 {
		if _, _, err := svc.RecordPendingTelemtUpdateFailure(ctx, "agent-1", "3.4.25"); err != nil {
			t.Fatalf("RecordPendingTelemtUpdateFailure: %v", err)
		}
	}
	if err := svc.SetPendingTelemtUpdate(ctx, "agent-1", "3.4.26"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate (new click): %v", err)
	}

	failures, gaveUp, err := svc.RecordPendingTelemtUpdateFailure(ctx, "agent-1", "3.4.26")
	if err != nil {
		t.Fatalf("RecordPendingTelemtUpdateFailure after new click: %v", err)
	}
	if gaveUp || failures != 1 {
		t.Fatalf("a new click must restart the budget, got failures=%d gaveUp=%v", failures, gaveUp)
	}
}

func TestLoadSettingsEmptyStoreReturnsDefaults(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	got, err := svc.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != DefaultSettings() {
		t.Fatalf("empty store should yield defaults, got %#v", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	want := Settings{
		CheckIntervalHours:  12,
		AutoUpdatePanel:     true,
		AutoUpdateAgents:    true,
		GitHubRepo:          "acme/panvex",
		GitHubToken:         "ghp_x",
		AgentDownloadSource: "mirror",
	}
	if err := svc.SaveSettings(context.Background(), want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := svc.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestStateRoundTripAndEmptyIsZero(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	zero, err := svc.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	// State is no longer comparable with == (TelemtAvailableVersions is a
	// slice): compare fields via reflect.DeepEqual instead. An empty store
	// still yields an otherwise-zero State, but LoadState normalizes
	// TelemtAvailableVersions to a non-nil empty slice (see
	// TestLoadStateNormalizesNilAvailableVersions for the dedicated case).
	wantZero := State{TelemtAvailableVersions: []string{}}
	if !reflect.DeepEqual(zero, wantZero) {
		t.Fatalf("empty store should yield zero State, got %#v want %#v", zero, wantZero)
	}
	want := State{
		LatestPanelVersion:      "1.2.3",
		PanelDownloadURL:        "https://x/panel",
		LastCheckedAt:           1730000000,
		LastCheckError:          "rate limited",
		TelemtAvailableVersions: []string{"3.4.25", "3.4.24"},
	}
	if err := svc.SaveState(context.Background(), want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := svc.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("state round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

// TestLoadStateNormalizesNilAvailableVersions covers the brief's LoadState
// normalization contract: a state blob persisted before
// TelemtAvailableVersions existed (or one where a successful check found no
// candidates, which json.Marshal's zero-value nil slice renders the same
// way) must load back with a non-nil empty slice, never nil — a nil slice
// would marshal as JSON null through the HTTP layer, breaking the frontend's
// z.array() contract.
func TestLoadStateNormalizesNilAvailableVersions(t *testing.T) {
	t.Parallel()
	store := &memStore{
		state: json.RawMessage(`{"latest_panel_version":"1.2.3","last_checked_at":1730000000}`),
	}
	svc := NewService(store)
	got, err := svc.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.TelemtAvailableVersions == nil {
		t.Fatal("TelemtAvailableVersions = nil, want non-nil empty slice")
	}
	if len(got.TelemtAvailableVersions) != 0 {
		t.Fatalf("TelemtAvailableVersions = %v, want empty", got.TelemtAvailableVersions)
	}
	if got.LatestPanelVersion != "1.2.3" {
		t.Fatalf("LatestPanelVersion = %q, want %q", got.LatestPanelVersion, "1.2.3")
	}
}
