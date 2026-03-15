package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/security"
)

func TestServerLoginSetsSessionAndReturnsMe(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST /api/auth/login returned no cookies")
	}

	meResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, want %d", meResponse.Code, http.StatusOK)
	}

	var payload struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(meResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.Username != "viewer" {
		t.Fatalf("payload.Username = %q, want %q", payload.Username, "viewer")
	}

	if payload.Role != string(auth.RoleViewer) {
		t.Fatalf("payload.Role = %q, want %q", payload.Role, auth.RoleViewer)
	}
}

func TestServerCreateJobRejectsViewerRole(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	server.agents["agent-1"] = Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		EnvironmentID:"prod",
		FleetGroupID: "ams-1",
		ReadOnly:     false,
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)

	jobResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/jobs", map[string]any{
		"action":           "runtime.reload",
		"target_agent_ids": []string{"agent-1"},
		"idempotency_key":  "job-1",
		"ttl_seconds":      60,
	}, loginResponse.Result().Cookies())
	if jobResponse.Code != http.StatusForbidden {
		t.Fatalf("POST /api/jobs status = %d, want %d", jobResponse.Code, http.StatusForbidden)
	}
}

func TestServerCreateJobAcceptsOperatorWithTotp(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	_, secret, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	server.agents["agent-1"] = Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		EnvironmentID:"prod",
		FleetGroupID: "ams-1",
		ReadOnly:     false,
	}

	code, err := server.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "operator",
		"password":  "operator-password",
		"totp_code": code,
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	jobResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/jobs", map[string]any{
		"action":           "runtime.reload",
		"target_agent_ids": []string{"agent-1"},
		"idempotency_key":  "job-1",
		"ttl_seconds":      60,
	}, loginResponse.Result().Cookies())
	if jobResponse.Code != http.StatusAccepted {
		t.Fatalf("POST /api/jobs status = %d, want %d", jobResponse.Code, http.StatusAccepted)
	}
}

func TestServerNewDoesNotReseedExistingStoreUsers(t *testing.T) {
	now := time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	seeded := auth.NewServiceWithStore(store)
	user, _, err := seeded.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "current-password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	server := New(Options{
		Now: func() time.Time { return now.Add(time.Minute) },
		Users: []auth.User{
			{
				ID:           user.ID,
				Username:     user.Username,
				PasswordHash: "stale-hash",
				Role:         user.Role,
				CreatedAt:    user.CreatedAt,
			},
		},
		Store: store,
	})

	if _, err := server.auth.Authenticate(auth.LoginInput{
		Username: "admin",
		Password: "current-password",
	}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Authenticate() with stored password error = %v", err)
	}

	if _, err := server.auth.Authenticate(auth.LoginInput{
		Username: "admin",
		Password: "stale-password",
	}, now.Add(2*time.Minute)); err != auth.ErrInvalidCredentials {
		t.Fatalf("Authenticate() with stale password error = %v, want %v", err, auth.ErrInvalidCredentials)
	}
}

func TestHTTPFleetInventoryAndMetricsSurviveRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	bootstrap := auth.NewService()
	user, _, err := bootstrap.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	first := New(Options{
		Now: func() time.Time { return now },
		Users: []auth.User{user},
		Store: store,
	})
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := first.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	first.applyAgentSnapshot(agentSnapshot{
		AgentID:       identity.AgentID,
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		Instances: []instanceSnapshot{
			{
				ID:                "instance-1",
				Name:              "telemt-a",
				Version:           "2026.03",
				ConfigFingerprint: "cfg-1",
				ConnectedUsers:    42,
			},
		},
		Metrics: map[string]uint64{
			"requests_total": 128,
		},
		ObservedAt: now.Add(15 * time.Second),
	})

	restored := New(Options{
		Now: func() time.Time { return now.Add(2 * time.Minute) },
		Users: []auth.User{user},
		Store: store,
	})
	loginResponse := performJSONRequest(t, restored.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	fleetHTTPResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/fleet", nil, cookies)
	if fleetHTTPResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/fleet status = %d, want %d", fleetHTTPResponse.Code, http.StatusOK)
	}
	var fleetPayload fleetResponse
	if err := json.Unmarshal(fleetHTTPResponse.Body.Bytes(), &fleetPayload); err != nil {
		t.Fatalf("json.Unmarshal(fleet) error = %v", err)
	}
	if fleetPayload.TotalAgents != 1 {
		t.Fatalf("fleet.TotalAgents = %d, want %d", fleetPayload.TotalAgents, 1)
	}
	if fleetPayload.TotalInstances != 1 {
		t.Fatalf("fleet.TotalInstances = %d, want %d", fleetPayload.TotalInstances, 1)
	}
	if fleetPayload.MetricSnapshots != 1 {
		t.Fatalf("fleet.MetricSnapshots = %d, want %d", fleetPayload.MetricSnapshots, 1)
	}

	agentsResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/agents", nil, cookies)
	if agentsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/agents status = %d, want %d", agentsResponse.Code, http.StatusOK)
	}
	var agentsPayload []Agent
	if err := json.Unmarshal(agentsResponse.Body.Bytes(), &agentsPayload); err != nil {
		t.Fatalf("json.Unmarshal(agents) error = %v", err)
	}
	if len(agentsPayload) != 1 {
		t.Fatalf("len(agents) = %d, want %d", len(agentsPayload), 1)
	}

	instancesResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/instances", nil, cookies)
	if instancesResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/instances status = %d, want %d", instancesResponse.Code, http.StatusOK)
	}
	var instancesPayload []Instance
	if err := json.Unmarshal(instancesResponse.Body.Bytes(), &instancesPayload); err != nil {
		t.Fatalf("json.Unmarshal(instances) error = %v", err)
	}
	if len(instancesPayload) != 1 {
		t.Fatalf("len(instances) = %d, want %d", len(instancesPayload), 1)
	}

	metricsResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/metrics", nil, cookies)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/metrics status = %d, want %d", metricsResponse.Code, http.StatusOK)
	}
	var metricsPayload []MetricSnapshot
	if err := json.Unmarshal(metricsResponse.Body.Bytes(), &metricsPayload); err != nil {
		t.Fatalf("json.Unmarshal(metrics) error = %v", err)
	}
	if len(metricsPayload) != 1 {
		t.Fatalf("len(metrics) = %d, want %d", len(metricsPayload), 1)
	}
}

func TestHTTPJobsAndAuditSurviveRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 11, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	bootstrap := auth.NewService()
	user, secret, err := bootstrap.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	first := New(Options{
		Now: func() time.Time { return now },
		Users: []auth.User{user},
		Store: store,
	})

	tokenOne, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken(agent-1) error = %v", err)
	}
	agentOne, err := first.enrollAgent(agentEnrollmentRequest{
		Token:    tokenOne.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent(agent-1) error = %v", err)
	}

	tokenTwo, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken(agent-2) error = %v", err)
	}
	agentTwo, err := first.enrollAgent(agentEnrollmentRequest{
		Token:    tokenTwo.Value,
		NodeName: "node-b",
		Version:  "1.0.0",
	}, now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent(agent-2) error = %v", err)
	}

	loginCode, err := first.auth.GenerateTotpCode(secret, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode(first) error = %v", err)
	}
	loginResponse := performJSONRequest(t, first.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "operator",
		"password":  "operator-password",
		"totp_code": loginCode,
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	jobResponse := performJSONRequest(t, first.Handler(), http.MethodPost, "/api/jobs", map[string]any{
		"action":           "runtime.reload",
		"target_agent_ids": []string{agentOne.AgentID, agentTwo.AgentID},
		"idempotency_key":  "reload-both",
		"ttl_seconds":      60,
	}, loginResponse.Result().Cookies())
	if jobResponse.Code != http.StatusAccepted {
		t.Fatalf("POST /api/jobs status = %d, want %d", jobResponse.Code, http.StatusAccepted)
	}

	var createdJob jobs.Job
	if err := json.Unmarshal(jobResponse.Body.Bytes(), &createdJob); err != nil {
		t.Fatalf("json.Unmarshal(job) error = %v", err)
	}

	first.markJobDelivered(agentOne.AgentID, createdJob.ID)
	first.markJobDelivered(agentTwo.AgentID, createdJob.ID)
	first.recordJobResult(agentOne.AgentID, createdJob.ID, true, "ok", now.Add(15*time.Second))
	first.recordJobResult(agentTwo.AgentID, createdJob.ID, false, "reload failed", now.Add(16*time.Second))

	restored := New(Options{
		Now: func() time.Time { return now.Add(2 * time.Minute) },
		Users: []auth.User{user},
		Store: store,
	})
	restoredCode, err := restored.auth.GenerateTotpCode(secret, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("GenerateTotpCode(restored) error = %v", err)
	}
	restoredLoginResponse := performJSONRequest(t, restored.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "operator",
		"password":  "operator-password",
		"totp_code": restoredCode,
	}, nil)
	if restoredLoginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login restored status = %d, want %d", restoredLoginResponse.Code, http.StatusOK)
	}
	cookies := restoredLoginResponse.Result().Cookies()

	jobsResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/jobs", nil, cookies)
	if jobsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/jobs status = %d, want %d", jobsResponse.Code, http.StatusOK)
	}
	var jobsPayload []jobs.Job
	if err := json.Unmarshal(jobsResponse.Body.Bytes(), &jobsPayload); err != nil {
		t.Fatalf("json.Unmarshal(jobs) error = %v", err)
	}
	if len(jobsPayload) != 1 {
		t.Fatalf("len(jobs) = %d, want %d", len(jobsPayload), 1)
	}
	if jobsPayload[0].Status != jobs.StatusFailed {
		t.Fatalf("jobs[0].Status = %q, want %q", jobsPayload[0].Status, jobs.StatusFailed)
	}
	if len(jobsPayload[0].Targets) != 2 {
		t.Fatalf("len(jobs[0].Targets) = %d, want %d", len(jobsPayload[0].Targets), 2)
	}

	auditResponse := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit status = %d, want %d", auditResponse.Code, http.StatusOK)
	}
	var auditPayload []AuditEvent
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &auditPayload); err != nil {
		t.Fatalf("json.Unmarshal(audit) error = %v", err)
	}

	var sawCreate bool
	var sawResult bool
	for _, event := range auditPayload {
		if event.Action == "jobs.create" {
			sawCreate = true
		}
		if event.Action == "jobs.result" {
			sawResult = true
		}
	}
	if !sawCreate {
		t.Fatal("audit trail missing jobs.create event")
	}
	if !sawResult {
		t.Fatal("audit trail missing jobs.result event")
	}
}

func TestHTTPEmbeddedUIFallsBackToIndexForSPARoute(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	server := New(Options{
		Now:     func() time.Time { return now },
		UIFiles: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html><body>panvex</body></html>")},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log('panvex')")},
		},
	})

	response := performRequest(t, server.Handler(), http.MethodGet, "/fleet/agent-1", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /fleet/agent-1 status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Result().Header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("GET /fleet/agent-1 Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}
	if body := response.Body.String(); body != "<html><body>panvex</body></html>" {
		t.Fatalf("GET /fleet/agent-1 body = %q, want embedded index", body)
	}
}

func TestHTTPEmbeddedUIServesStaticAsset(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		UIFiles: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html><body>panvex</body></html>")},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log('panvex')")},
		},
	})

	response := performRequest(t, server.Handler(), http.MethodGet, "/assets/app.js", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.js status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Result().Header.Get("Content-Type"); contentType != "text/javascript; charset=utf-8" {
		t.Fatalf("GET /assets/app.js Content-Type = %q, want %q", contentType, "text/javascript; charset=utf-8")
	}
	if body := response.Body.String(); body != "console.log('panvex')" {
		t.Fatalf("GET /assets/app.js body = %q, want embedded asset", body)
	}
}

func TestHTTPEmbeddedUIDoesNotShadowAPIRoutes(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		UIFiles: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html><body>panvex</body></html>")},
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)

	meResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/auth/me", nil, loginResponse.Result().Cookies())
	if meResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, want %d", meResponse.Code, http.StatusOK)
	}
	if contentType := meResponse.Result().Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("GET /api/auth/me Content-Type = %q, want %q", contentType, "application/json")
	}
}

func TestHTTPWithoutEmbeddedUIStillReturnsAPIOnlyNotFound(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	server := New(Options{
		Now:     func() time.Time { return now },
		UIFiles: nil,
	})

	response := performRequest(t, server.Handler(), http.MethodGet, "/app", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("GET /app status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func performJSONRequest(t *testing.T, handler http.Handler, method string, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		payload = encoded
	}

	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func performRequest(t *testing.T, handler http.Handler, method string, path string, body *bytes.Reader) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = body
	}

	request := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
