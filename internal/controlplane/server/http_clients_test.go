package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPClientsCreateTracksDeploymentsAndStructuredJobPayload(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000001",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})
	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "ams-2",
		Name:      "AMS-2",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000002",
		NodeName:     "node-b",
		FleetGroupID: "ams-2",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server.Handler())
	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients", map[string]any{
		"name":               "alice",
		"enabled":            true,
		"max_tcp_conns":      4,
		"max_unique_ips":     2,
		"data_quota_bytes":   int64(1024),
		"expiration_rfc3339": "2026-03-31T00:00:00Z",
		"fleet_group_ids":    []string{"default"},
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

	server.recordJobResult("agent-000001", enqueuedJobs[0].ID, true, "applied", `{"connection_link":"tg://proxy?server=node-a&secret=alice"}`, now.Add(time.Minute))

	detailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/clients/"+created.ID, nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/clients/{id} status = %d, want %d", detailResponse.Code, http.StatusOK)
	}

	var detail struct {
		ID          string `json:"id"`
		Deployments []struct {
			AgentID        string `json:"agent_id"`
			Status         string `json:"status"`
			ConnectionLink string `json:"connection_link"`
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
	if detail.Deployments[0].ConnectionLink != "tg://proxy?server=node-a&secret=alice" {
		t.Fatalf("detail.deployments[0].connection_link = %q, want %q", detail.Deployments[0].ConnectionLink, "tg://proxy?server=node-a&secret=alice")
	}
}

func TestHTTPClientsUpdateRotateAndDeleteQueueLifecycleJobs(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 30, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000001",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server.Handler())
	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"enabled":         true,
		"fleet_group_ids": []string{"default"},
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

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/clients/"+created.ID, map[string]any{
		"name":               "alice-renamed",
		"enabled":            true,
		"max_tcp_conns":      9,
		"max_unique_ips":     5,
		"data_quota_bytes":   int64(2048),
		"expiration_rfc3339": "2026-04-30T00:00:00Z",
		"fleet_group_ids":    []string{"default"},
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

	rotateResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients/"+created.ID+"/rotate-secret", nil, cookies)
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

	deleteResponse := performJSONRequest(t, server.Handler(), http.MethodDelete, "/api/clients/"+created.ID, nil, cookies)
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
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	cookies := loginAdminForClients(t, server.Handler())
	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients", map[string]any{
		"name":        "alice",
		"user_ad_tag": "not-hex",
	}, cookies)
	if createResponse.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/clients invalid user_ad_tag status = %d, want %d", createResponse.Code, http.StatusBadRequest)
	}
}

func TestHTTPClientsAggregateUsageAcrossAgentSnapshots(t *testing.T) {
	now := time.Date(2026, time.March, 17, 17, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000001",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})
	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "ams-2",
		Name:      "AMS-2",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000002",
		NodeName:     "node-b",
		FleetGroupID: "ams-2",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server.Handler())
	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{"default", "ams-2"},
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

	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:    "agent-000001",
		NodeName:   "node-a",
		Version:    "dev",
		HasClients: true,
		Clients: []clientUsageSnapshot{
			{
				ClientID:         created.ID,
				TrafficUsedBytes: 1024,
				UniqueIPsUsed:    2,
				ActiveTCPConns:   3,
				ObservedAt:       now.Add(time.Minute),
			},
		},
		ObservedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(agent-000001) error = %v", err)
	}
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:    "agent-000002",
		NodeName:   "node-b",
		Version:    "dev",
		HasClients: true,
		Clients: []clientUsageSnapshot{
			{
				ClientID:         created.ID,
				TrafficUsedBytes: 512,
				UniqueIPsUsed:    1,
				ActiveTCPConns:   4,
				ObservedAt:       now.Add(2 * time.Minute),
			},
		},
		ObservedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(agent-000002) error = %v", err)
	}

	detailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/clients/"+created.ID, nil, cookies)
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

func TestHTTPClientsCreateReturnsInternalErrorWhenPersistenceFails(t *testing.T) {
	now := time.Date(2026, time.March, 18, 13, 30, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingStore{Store: sqliteStore}
	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	seedClientTargetAgent(t, store, server, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-2 * time.Minute),
	}, storage.AgentRecord{
		ID:           "agent-000001",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	})

	store.putClientErr = errors.New("put client failed")

	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/clients", map[string]any{
		"name":            "alice",
		"fleet_group_ids": []string{"default"},
	}, loginAdminForClients(t, server.Handler()))
	if createResponse.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/clients status = %d, want %d", createResponse.Code, http.StatusInternalServerError)
	}
}

func loginAdminForClients(t *testing.T, handler http.Handler) []*http.Cookie {
	t.Helper()

	loginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	return loginResponse.Result().Cookies()
}

func seedClientTargetAgent(t *testing.T, store storage.Store, server *Server, group storage.FleetGroupRecord, agent storage.AgentRecord) {
	t.Helper()

	ctx := context.Background()
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server.mu.Lock()
	server.agents[agent.ID] = agentFromRecord(agent)
	server.mu.Unlock()
}
