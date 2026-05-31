// Phase 3 (reset-quota) tests for the panel-side plumbing introduced
// alongside `last_reset_epoch_secs` persistence on client_deployments
// and drift detection in the usage-snapshot hot path.

package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestAdvanceDeploymentsFromTelemtResetThreeBranches drives the three
// outcomes the Phase 3 plan calls out for the per-snapshot drift
// comparison:
//
//   - in sync   → no deployment update
//   - telemt newer → deployment advances to match Telemt
//   - panel newer  → deployment left untouched (the API computes a
//     `quota_reset_drift` flag at response time)
//
// This is the heart of Phase 3: keep it green and the rest of the
// pipeline can be wired forward without surprise.
func TestAdvanceDeploymentsFromTelemtResetThreeBranches(t *testing.T) {
	const (
		clientID = "client-1"
		agentID  = "agent-A"
	)
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name           string
		panelTimestamp uint64
		telemtUnix     uint64
		wantChanged    bool
		wantNewPanel   uint64
	}{
		{
			name:           "in sync — no update",
			panelTimestamp: 1_700_000_000,
			telemtUnix:     1_700_000_000,
			wantChanged:    false,
			wantNewPanel:   1_700_000_000,
		},
		{
			name:           "telemt newer — adopt newer value",
			panelTimestamp: 1_700_000_000,
			telemtUnix:     1_700_001_000,
			wantChanged:    true,
			wantNewPanel:   1_700_001_000,
		},
		{
			name:           "panel newer — leave deployment untouched (drift surfaces at API)",
			panelTimestamp: 1_700_001_000,
			telemtUnix:     1_700_000_000,
			wantChanged:    false,
			wantNewPanel:   1_700_001_000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
			if err != nil {
				t.Fatalf("sqlite.Open() error = %v", err)
			}
			defer baseStore.Close()
			server := mustNew(t, Options{
				LoginTimingFloor: -1,
				Now:              func() time.Time { return now },
				Store:            baseStore,
			})
			defer server.Close()

			seedMirrorClient(t, server, managedClient{
				ID:        clients.ClientID(clientID),
				Name:      "alice",
				Enabled:   true,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil, []managedClientDeployment{{
				ClientID:           clients.ClientID(clientID),
				AgentID:            agentID,
				Status:             clientDeploymentStatusSucceeded,
				UpdatedAt:          now,
				LastResetEpochSecs: tc.panelTimestamp,
			}})

			snapshot := []clientUsageSnapshot{{
				ClientID:           clients.ClientID(clientID),
				QuotaLastResetUnix: tc.telemtUnix,
				ObservedAt:         now,
			}}

			changed := server.advanceDeploymentsFromTelemtReset(agentID, snapshot)
			// advanceDeploymentsFromTelemtReset only computes the changed
			// deployments; the actual mirror write happens via PersistDeployment.
			// For this unit test we assert against the returned change set and
			// the (possibly unchanged) mirror deployment.
			deployment := mirrorDeployment(server, clientID, agentID)
			if len(changed) > 0 {
				deployment = changed[0]
			}

			if tc.wantChanged && len(changed) == 0 {
				t.Fatalf("advanceDeploymentsFromTelemtReset returned no changes; want one")
			}
			if !tc.wantChanged && len(changed) != 0 {
				t.Fatalf("advanceDeploymentsFromTelemtReset returned %d changes; want zero", len(changed))
			}
			if deployment.LastResetEpochSecs != tc.wantNewPanel {
				t.Fatalf("deployment.LastResetEpochSecs = %d; want %d", deployment.LastResetEpochSecs, tc.wantNewPanel)
			}
		})
	}
}

// TestApplyClientResetQuotaResultRecordsTimestamp confirms that a
// successful client.reset_quota job result stamps
// `LastResetEpochSecs` onto the in-memory deployment and persists the
// row through the clients service. The result_json shape mirrors the
// agent-side `clientResetQuotaJobResult` struct.
func TestApplyClientResetQuotaResultRecordsTimestamp(t *testing.T) {
	const (
		clientID = "client-1"
		agentID  = "agent-A"
	)
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	resetUnix := uint64(now.Unix())

	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, baseStore, "default", now.Add(-time.Hour))
	if err := baseStore.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	// Seed the client row so the FK on client_deployments resolves
	// when the persistence path fires. The PutClient ciphertext value
	// is unrelated to this test — we only need the row to exist.
	if err := baseStore.PutClient(ctx, storage.ClientRecord{
		ID:               clientID,
		Name:             "alice",
		SecretCiphertext: "ciphertext",
		CreatedAt:        now.Add(-time.Hour),
		UpdatedAt:        now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            baseStore,
	})
	defer server.Close()

	seedMirrorClient(t, server, managedClient{
		ID:        clients.ClientID(clientID),
		Name:      "alice",
		Secret:    "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}, nil, []managedClientDeployment{{
		ClientID:         clients.ClientID(clientID),
		AgentID:          agentID,
		DesiredOperation: string(jobs.ActionClientCreate),
		Status:           clientDeploymentStatusSucceeded,
		ConnectionLinks:  []string{"tg://existing-link"},
		UpdatedAt:        now.Add(-time.Hour),
	}})

	payload, err := json.Marshal(clientResetQuotaJobPayload{ClientID: clientID, Name: "alice"})
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	resultJSON, err := json.Marshal(clientResetQuotaJobResultPayload{
		UsedBytes:          0,
		LastResetEpochSecs: resetUnix,
	})
	if err != nil {
		t.Fatalf("json.Marshal result: %v", err)
	}
	job := jobs.Job{
		ID:             "job-reset-1",
		Action:         jobs.ActionClientResetQuota,
		TargetAgentIDs: []string{agentID},
		PayloadJSON:    string(payload),
	}

	server.applyClientResetQuotaResult(ctx, agentID, job, true, string(resultJSON), now)

	got := mirrorDeployment(server, clientID, agentID)
	if got.LastResetEpochSecs != resetUnix {
		t.Fatalf("in-memory LastResetEpochSecs = %d; want %d", got.LastResetEpochSecs, resetUnix)
	}
	// The reset_quota path must NOT rewrite the deployment's
	// desired-state metadata (status, connection links) — only the
	// reset timestamp moves. Without this guard, the panel's view of
	// the deployment would silently mutate every time the operator
	// hit "Reset".
	if got.Status != clientDeploymentStatusSucceeded {
		t.Fatalf("Status = %q; want %q (reset_quota must not touch status)", got.Status, clientDeploymentStatusSucceeded)
	}
	if len(got.ConnectionLinks) != 1 || got.ConnectionLinks[0] != "tg://existing-link" {
		t.Fatalf("ConnectionLinks = %v; want preserved", got.ConnectionLinks)
	}

	// Storage persistence: reading the deployment back from disk
	// should reflect the same LastResetEpochSecs.
	deployments, err := baseStore.ListClientDeployments(ctx, clientID)
	if err != nil {
		t.Fatalf("ListClientDeployments() error = %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("ListClientDeployments() len = %d, want 1", len(deployments))
	}
	if deployments[0].LastResetEpochSecs != resetUnix {
		t.Fatalf("storage LastResetEpochSecs = %d; want %d", deployments[0].LastResetEpochSecs, resetUnix)
	}
}

// TestApplyClientResetQuotaResultFailureLeavesDeploymentUntouched
// covers the typed-failure branches (unsupported_telemt /
// read_only_telemt) — on failure the panel records nothing on the
// deployment row; the per-target reason lives on the Job target's
// ResultJSON and is rendered by the UI from there. This guards against
// a stale or zeroed `last_reset_epoch_secs` ending up persisted.
func TestApplyClientResetQuotaResultFailureLeavesDeploymentUntouched(t *testing.T) {
	const (
		clientID = "client-1"
		agentID  = "agent-A"
	)
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)

	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            baseStore,
	})
	defer server.Close()

	prevPanelTS := uint64(1_700_000_000)
	seedMirrorClient(t, server, managedClient{
		ID:        clients.ClientID(clientID),
		Name:      "alice",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil, []managedClientDeployment{{
		ClientID:           clients.ClientID(clientID),
		AgentID:            agentID,
		Status:             clientDeploymentStatusSucceeded,
		LastResetEpochSecs: prevPanelTS,
		UpdatedAt:          now,
	}})

	payload, _ := json.Marshal(clientResetQuotaJobPayload{ClientID: clientID, Name: "alice"})
	failureResult := `{"used_bytes":0,"last_reset_epoch_secs":0,"unsupported_telemt":true}`
	job := jobs.Job{
		ID:             "job-reset-failed",
		Action:         jobs.ActionClientResetQuota,
		TargetAgentIDs: []string{agentID},
		PayloadJSON:    string(payload),
	}

	server.applyClientResetQuotaResult(context.Background(), agentID, job, false, failureResult, now)

	got := mirrorDeployment(server, clientID, agentID)
	if got.LastResetEpochSecs != prevPanelTS {
		t.Fatalf("LastResetEpochSecs = %d; want %d (failed reset must not bump the timestamp)", got.LastResetEpochSecs, prevPanelTS)
	}
}
