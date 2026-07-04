package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPClientsCreateTracksDeploymentsAndStructuredJobPayload(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	seedClientTargetAgent(t, store, server, "ams-2", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000002",
		NodeName:   "node-b",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":               "alice",
		"enabled":            true,
		"max_tcp_conns":      4,
		"max_unique_ips":     2,
		"data_quota_bytes":   int64(1024),
		"expiration_rfc3339": "2026-03-31T00:00:00Z",
		"fleet_group_ids":    []string{defaultGroupID},
		"agent_ids":          []string{"agent-000002"},
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var created struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Secret    string `json:"secret"`
		UserADTag string `json:"user_ad_tag"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("created.id = empty, want generated client id")
	}
	if created.Name != "alice" {
		t.Fatalf("created.name = %q, want %q", created.Name, "alice")
	}
	if created.Secret == "" {
		t.Fatal("created.secret = empty, want generated secret")
	}
	if len(created.Secret) != 32 {
		t.Fatalf("len(created.secret) = %d, want %d", len(created.Secret), 32)
	}
	if len(created.UserADTag) != 32 {
		t.Fatalf("len(created.user_ad_tag) = %d, want %d", len(created.UserADTag), 32)
	}

	enqueuedJobs := server.jobs.List()
	if len(enqueuedJobs) != 1 {
		t.Fatalf("len(server.jobs.List()) = %d, want %d", len(enqueuedJobs), 1)
	}
	if enqueuedJobs[0].Action != jobs.ActionClientCreate {
		t.Fatalf("jobs[0].Action = %q, want %q", enqueuedJobs[0].Action, jobs.ActionClientCreate)
	}
	if enqueuedJobs[0].PayloadJSON == "" {
		t.Fatal("jobs[0].PayloadJSON = empty, want structured client payload")
	}
	if len(enqueuedJobs[0].TargetAgentIDs) != 2 {
		t.Fatalf("len(jobs[0].TargetAgentIDs) = %d, want %d", len(enqueuedJobs[0].TargetAgentIDs), 2)
	}

	server.recordJobResult(context.Background(), "agent-000001", enqueuedJobs[0].ID, true, "applied", `{"connection_links":["tg://proxy?server=node-a&secret=alice"]}`, now.Add(time.Minute))

	detailResponse := performJSONRequest(t, server, http.MethodGet, "/api/clients/"+created.ID, nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/clients/{id} status = %d, want %d", detailResponse.Code, http.StatusOK)
	}

	var detail struct {
		ID          string `json:"id"`
		Deployments []struct {
			AgentID         string   `json:"agent_id"`
			Status          string   `json:"status"`
			ConnectionLinks []string `json:"connection_links"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal(detail) error = %v", err)
	}
	if detail.ID != created.ID {
		t.Fatalf("detail.id = %q, want %q", detail.ID, created.ID)
	}
	if len(detail.Deployments) != 2 {
		t.Fatalf("len(detail.deployments) = %d, want %d", len(detail.Deployments), 2)
	}
	if detail.Deployments[0].AgentID != "agent-000001" {
		t.Fatalf("detail.deployments[0].agent_id = %q, want %q", detail.Deployments[0].AgentID, "agent-000001")
	}
	if detail.Deployments[0].Status != "succeeded" {
		t.Fatalf("detail.deployments[0].status = %q, want %q", detail.Deployments[0].Status, "succeeded")
	}
	if got := detail.Deployments[0].ConnectionLinks; len(got) != 1 || got[0] != "tg://proxy?server=node-a&secret=alice" {
		t.Fatalf("detail.deployments[0].connection_links = %v, want [tg://proxy?server=node-a&secret=alice]", got)
	}
}

func TestHTTPClientsUpdateRotateAndDeleteQueueLifecycleJobs(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 30, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"enabled":         true,
		"fleet_group_ids": []string{defaultGroupID},
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var created struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}

	updateResponse := performJSONRequest(t, server, http.MethodPut, "/api/clients/"+created.ID, map[string]any{
		"name":               "alice-renamed",
		"enabled":            true,
		"max_tcp_conns":      9,
		"max_unique_ips":     5,
		"data_quota_bytes":   int64(2048),
		"expiration_rfc3339": "2026-04-30T00:00:00Z",
		"fleet_group_ids":    []string{defaultGroupID},
	}, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/clients/{id} status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	queuedJobs := server.jobs.List()
	if len(queuedJobs) != 2 {
		t.Fatalf("len(server.jobs.List()) after update = %d, want %d", len(queuedJobs), 2)
	}
	if queuedJobs[1].Action != jobs.ActionClientUpdate {
		t.Fatalf("jobs[1].Action = %q, want %q", queuedJobs[1].Action, jobs.ActionClientUpdate)
	}

	rotateResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients/"+created.ID+"/rotate-secret", nil, cookies)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/clients/{id}/rotate-secret status = %d, want %d", rotateResponse.Code, http.StatusOK)
	}

	var rotated struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(rotateResponse.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("json.Unmarshal(rotate) error = %v", err)
	}
	if rotated.Secret == "" {
		t.Fatal("rotated.secret = empty, want regenerated secret")
	}
	if len(rotated.Secret) != 32 {
		t.Fatalf("len(rotated.secret) = %d, want %d", len(rotated.Secret), 32)
	}
	if rotated.Secret == created.Secret {
		t.Fatal("rotated.secret = original secret, want changed secret")
	}

	queuedJobs = server.jobs.List()
	if len(queuedJobs) != 3 {
		t.Fatalf("len(server.jobs.List()) after rotate = %d, want %d", len(queuedJobs), 3)
	}
	if queuedJobs[2].Action != jobs.ActionClientRotateSecret {
		t.Fatalf("jobs[2].Action = %q, want %q", queuedJobs[2].Action, jobs.ActionClientRotateSecret)
	}

	deleteResponse := performJSONRequest(t, server, http.MethodDelete, "/api/clients/"+created.ID, nil, cookies)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/clients/{id} status = %d, want %d", deleteResponse.Code, http.StatusNoContent)
	}

	queuedJobs = server.jobs.List()
	if len(queuedJobs) != 4 {
		t.Fatalf("len(server.jobs.List()) after delete = %d, want %d", len(queuedJobs), 4)
	}
	if queuedJobs[3].Action != jobs.ActionClientDelete {
		t.Fatalf("jobs[3].Action = %q, want %q", queuedJobs[3].Action, jobs.ActionClientDelete)
	}

	storedClient, err := store.GetClientByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetClientByID() error = %v", err)
	}
	if storedClient.DeletedAt == nil {
		t.Fatal("storedClient.DeletedAt = nil, want soft delete timestamp")
	}
}

func TestHTTPClientsRejectInvalidUserADTag(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 40, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":        "alice",
		"user_ad_tag": "not-hex",
	}, cookies)
	if createResponse.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/clients invalid user_ad_tag status = %d, want %d", createResponse.Code, http.StatusBadRequest)
	}
}

// TestHTTPClientsRejectInvalidName verifies that a client name violating
// Telemt's username constraint ([A-Za-z0-9_.-], 1..64 chars) is rejected
// with 400 instead of being accepted server-side and failing irrecoverably
// on every node.
func TestHTTPClientsRejectInvalidName(t *testing.T) {
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	cookies := loginAdminForClients(t, server)

	cases := []struct {
		label string
		name  string
	}{
		{"contains space", "premium users"},
		{"contains slash", "premium/users"},
		{"contains cyrillic", "клиент"},
		{"too long", strings.Repeat("a", 65)},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			resp := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
				"name": tc.name,
			}, cookies)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("POST /api/clients name=%q status = %d, want %d", tc.name, resp.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHTTPClientsAggregateUsageAcrossAgentSnapshots(t *testing.T) {
	now := time.Date(2026, time.March, 17, 17, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	ams2GroupID := seedClientTargetAgent(t, store, server, "ams-2", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000002",
		NodeName:   "node-b",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{defaultGroupID, ams2GroupID},
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:     "agent-000001",
		AgentBootID: "boot-1",
		NodeName:    "node-a",
		Version:     "dev",
		HasClients:  true,
		Clients: []clients.UsageReport{
			{
				ClientID:       clients.ClientID(created.ID),
				TotalBytes:     1024,
				UniqueIPsUsed:  2,
				ActiveTCPConns: 3,
				ObservedAt:     now.Add(time.Minute),
			},
		},
		ObservedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(agent-000001) error = %v", err)
	}
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:     "agent-000002",
		AgentBootID: "boot-1",
		NodeName:    "node-b",
		Version:     "dev",
		HasClients:  true,
		Clients: []clients.UsageReport{
			{
				ClientID:       clients.ClientID(created.ID),
				TotalBytes:     512,
				UniqueIPsUsed:  1,
				ActiveTCPConns: 4,
				ObservedAt:     now.Add(2 * time.Minute),
			},
		},
		ObservedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(agent-000002) error = %v", err)
	}

	detailResponse := performJSONRequest(t, server, http.MethodGet, "/api/clients/"+created.ID, nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/clients/{id} status = %d, want %d", detailResponse.Code, http.StatusOK)
	}

	var detail struct {
		TrafficUsedBytes uint64 `json:"traffic_used_bytes"`
		UniqueIPsUsed    int    `json:"unique_ips_used"`
		ActiveTCPConns   int    `json:"active_tcp_conns"`
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal(detail) error = %v", err)
	}
	if detail.TrafficUsedBytes != 1536 {
		t.Fatalf("detail.traffic_used_bytes = %d, want %d", detail.TrafficUsedBytes, 1536)
	}
	if detail.UniqueIPsUsed != 3 {
		t.Fatalf("detail.unique_ips_used = %d, want %d", detail.UniqueIPsUsed, 3)
	}
	if detail.ActiveTCPConns != 7 {
		t.Fatalf("detail.active_tcp_conns = %d, want %d", detail.ActiveTCPConns, 7)
	}
}

// TestHTTPClientsListingReflectsDeploymentAndUsage is a characterization
// test (Task D1) that pins the GET /api/clients listing response shape:
// after a client is created, its single deployment job completes, and a
// usage snapshot arrives, the listing row must carry the recorded usage
// (traffic/unique-ips/tcp-conns) and the deployment-derived
// last_deploy_status. It locks the listing behaviour so repointing
// listClientsListingSnapshot at the clients.Service mirror cannot drift.
func TestHTTPClientsListingReflectsDeploymentAndUsage(t *testing.T) {
	now := time.Date(2026, time.March, 19, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{defaultGroupID},
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusCreated)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}

	enqueuedJobs := server.jobs.List()
	if len(enqueuedJobs) != 1 {
		t.Fatalf("len(server.jobs.List()) = %d, want %d", len(enqueuedJobs), 1)
	}
	server.recordJobResult(context.Background(), "agent-000001", enqueuedJobs[0].ID, true, "applied", `{"connection_links":["tg://proxy?server=node-a&secret=alice"]}`, now.Add(time.Minute))

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:     "agent-000001",
		AgentBootID: "boot-1",
		NodeName:    "node-a",
		Version:     "dev",
		HasClients:  true,
		Clients: []clients.UsageReport{
			{
				ClientID:       clients.ClientID(created.ID),
				TotalBytes:     1024,
				UniqueIPsUsed:  2,
				ActiveTCPConns: 3,
				ObservedAt:     now.Add(time.Minute),
			},
		},
		ObservedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(agent-000001) error = %v", err)
	}

	listResponse := performJSONRequest(t, server, http.MethodGet, "/api/clients", nil, cookies)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/clients status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var listing []struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		TrafficUsedBytes uint64 `json:"traffic_used_bytes"`
		UniqueIPsUsed    int    `json:"unique_ips_used"`
		ActiveTCPConns   int    `json:"active_tcp_conns"`
		LastDeployStatus string `json:"last_deploy_status"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listing); err != nil {
		t.Fatalf("json.Unmarshal(listing) error = %v", err)
	}
	if len(listing) != 1 {
		t.Fatalf("len(listing) = %d, want %d", len(listing), 1)
	}
	row := listing[0]
	if row.ID != created.ID {
		t.Fatalf("listing[0].id = %q, want %q", row.ID, created.ID)
	}
	if row.TrafficUsedBytes != 1024 {
		t.Fatalf("listing[0].traffic_used_bytes = %d, want %d", row.TrafficUsedBytes, 1024)
	}
	if row.UniqueIPsUsed != 2 {
		t.Fatalf("listing[0].unique_ips_used = %d, want %d", row.UniqueIPsUsed, 2)
	}
	if row.ActiveTCPConns != 3 {
		t.Fatalf("listing[0].active_tcp_conns = %d, want %d", row.ActiveTCPConns, 3)
	}
	if row.LastDeployStatus != "succeeded" {
		t.Fatalf("listing[0].last_deploy_status = %q, want %q", row.LastDeployStatus, "succeeded")
	}
}

// TestClientsServiceMirrorConsistentAfterWritePaths exercises the three
// server write-paths B3 hardened — usage-snapshot apply, client job
// deployment result, and reset-quota result — and asserts the
// clients.Service mirror (read directly via MirrorSnapshot, the same
// source the repointed HTTP listing/detail use) reflects every change.
// This is the gate for C1: if the mirror diverges here, deleting the
// server maps would change behaviour.
func TestClientsServiceMirrorConsistentAfterWritePaths(t *testing.T) {
	now := time.Date(2026, time.May, 31, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	cookies := loginAdminForClients(t, server)
	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{defaultGroupID},
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusCreated)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}

	// (1) Job-deployment write-path: record a successful client-create job.
	createJobs := server.jobs.List()
	if len(createJobs) != 1 {
		t.Fatalf("len(create jobs) = %d, want 1", len(createJobs))
	}
	server.recordJobResult(context.Background(), "agent-000001", createJobs[0].ID, true, "applied",
		`{"connection_links":["tg://proxy?server=node-a&secret=alice"]}`, now.Add(time.Minute))

	// (2) Usage-snapshot write-path: apply a live usage tick.
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:     "agent-000001",
		AgentBootID: "boot-1",
		NodeName:    "node-a",
		Version:     "dev",
		HasClients:  true,
		Clients: []clients.UsageReport{
			{
				ClientID:       clients.ClientID(created.ID),
				TotalBytes:     2048,
				UniqueIPsUsed:  4,
				ActiveTCPConns: 5,
				ObservedAt:     now.Add(time.Minute),
			},
		},
		ObservedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	// (3) Reset-quota write-path: enqueue + record a successful reset.
	resetResponse := performJSONRequest(t, server, http.MethodPost,
		"/api/clients/"+created.ID+"/reset-quota", nil, cookies)
	if resetResponse.Code != http.StatusOK && resetResponse.Code != http.StatusAccepted {
		t.Fatalf("POST reset-quota status = %d, want 200/202", resetResponse.Code)
	}
	var resetJobID string
	for _, j := range server.jobs.List() {
		if j.Action == jobs.ActionClientResetQuota {
			resetJobID = j.ID
		}
	}
	if resetJobID == "" {
		t.Fatal("no client.reset_quota job enqueued")
	}
	const resetEpoch = 1748682000 // arbitrary epoch newer than zero
	server.recordJobResult(context.Background(), "agent-000001", resetJobID, true, "reset",
		`{"last_reset_epoch_secs":1748682000}`, now.Add(2*time.Minute))

	// Assert the Service mirror reflects all three write-paths.
	mirror := server.clientsSvc.MirrorSnapshot()
	cid := clients.ClientID(created.ID)

	usage, ok := mirror.Usage[cid]["agent-000001"]
	if !ok {
		t.Fatalf("mirror.Usage[%s][agent-000001] missing", created.ID)
	}
	if usage.TrafficUsedBytes != 2048 || usage.UniqueIPsUsed != 4 || usage.ActiveTCPConns != 5 {
		t.Fatalf("mirror usage = %+v, want traffic=2048 ips=4 conns=5", usage)
	}

	deployment, ok := mirror.Deployments[cid]["agent-000001"]
	if !ok {
		t.Fatalf("mirror.Deployments[%s][agent-000001] missing", created.ID)
	}
	if deployment.Status != clientDeploymentStatusSucceeded {
		t.Fatalf("mirror deployment status = %q, want succeeded", deployment.Status)
	}
	if deployment.LastResetEpochSecs != resetEpoch {
		t.Fatalf("mirror deployment LastResetEpochSecs = %d, want %d", deployment.LastResetEpochSecs, resetEpoch)
	}
}

func TestHTTPClientsCreateReturnsInternalErrorWhenPersistenceFails(t *testing.T) {
	now := time.Date(2026, time.March, 18, 13, 30, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingStore{MigrationStore: sqliteStore}
	// After Wave 4.2 the production path is clientsSvc.SaveState →
	// uow.Do → clients.Repository.Save, not s.store.PutClient. Failure is
	// injected at the Repository layer via failingClientsRepository and
	// wired through Options.ClientsRepoOverride. The saveErr is set after
	// server construction (same timing as the old store.putClientErr) so
	// that setup writes (fleet group, agent seed) still succeed.
	failingRepo := &failingClientsRepository{
		Repository: sqlite.NewClientsRepository(sqliteStore.DB()),
	}
	server := mustNew(t, Options{
		LoginTimingFloor:    -1,
		Now:                 func() time.Time { return now },
		Store:               store,
		ClientsRepoOverride: failingRepo,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	failingRepo.saveErr = errors.New("put client failed")

	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{defaultGroupID},
	}, loginAdminForClients(t, server))
	if createResponse.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusInternalServerError)
	}
}

func TestRecordClientJobResultDoesNotPanicWhenDeploymentPersistenceFails(t *testing.T) {
	now := time.Date(2026, time.March, 19, 9, 15, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingStore{MigrationStore: sqliteStore}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	client, _, _, err := server.createClient(context.Background(), "user-000001", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{defaultGroupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient() error = %v", err)
	}

	jobList := server.jobs.List()
	if len(jobList) != 1 {
		t.Fatalf("len(jobs.List()) = %d, want %d", len(jobList), 1)
	}

	store.putClientDeploymentErr = errors.New("put client deployment failed")

	server.recordClientJobResultWithContext(t.Context(), "agent-000001", jobList[0].ID, true, "ok", `{"connection_links":["tg://proxy?secret=abc"]}`, now.Add(time.Minute))

	detailClient, _, deployments, err := server.clientDetailSnapshot(string(client.ID))
	if err != nil {
		t.Fatalf("clientDetailSnapshot() error = %v", err)
	}
	if detailClient.ID != client.ID {
		t.Fatalf("detailClient.ID = %q, want %q", detailClient.ID, client.ID)
	}
	if len(deployments) != 1 {
		t.Fatalf("len(deployments) = %d, want %d", len(deployments), 1)
	}
}

func loginAdminForClients(t *testing.T, srv *Server) []*http.Cookie {
	t.Helper()

	loginResponse := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	return loginResponse.Result().Cookies()
}

// seedClientTargetAgent upserts a fleet-group row by name + an agent
// row tied to it, then mirrors the agent into the server's in-memory
// map. Returns the generated fleet-group UUID so callers can pass it
// into client-mutation requests (fleet_group_ids) that reference the
// same group. The caller's AgentRecord.FleetGroupID is overwritten
// with this UUID before persistence so the FK resolves.
func seedClientTargetAgent(t *testing.T, store storage.Store, server *Server, groupName string, groupCreatedAt time.Time, agent storage.AgentRecord) string {
	t.Helper()

	fleetGroupID := seedTestFleetGroup(t, store, groupName, groupCreatedAt)

	ctx := context.Background()
	agent.FleetGroupID = fleetGroupID
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server.mu.Lock()
	server.seedLiveAgentKeyed(agent.ID, agentFromRecord(agent))
	server.mu.Unlock()
	return fleetGroupID
}

func TestCreateClientGeneratesSubscriptionToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	created, _, _, err := server.createClient(context.Background(), "user-000001", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient: %v", err)
	}
	if created.SubscriptionToken == "" {
		t.Fatal("expected non-empty SubscriptionToken after createClient")
	}
}

func TestSubscriptionURLForBuildsAndGuards(t *testing.T) {
	s := &Server{}
	if got := s.subscriptionURLFor("tok123"); got != "" {
		t.Fatalf("no base configured: got %q, want empty", got)
	}
	s.SetSubscriptionListener(":8081", "https://sub.example.com/")
	if got := s.subscriptionURLFor("tok123"); got != "https://sub.example.com/sub/tok123" {
		t.Fatalf("got %q", got)
	}
	if got := s.subscriptionURLFor(""); got != "" {
		t.Fatalf("empty token: got %q, want empty", got)
	}
}
