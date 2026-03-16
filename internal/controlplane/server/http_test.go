package server

import (
	"bytes"
	"context"
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
		Username    string `json:"username"`
		Role        string `json:"role"`
		TotpEnabled bool   `json:"totp_enabled"`
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

	if payload.TotpEnabled {
		t.Fatal("payload.TotpEnabled = true, want false")
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
	user, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
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

	secret, err := server.auth.StartTotpSetup(user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := server.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	if _, err := server.auth.EnableTotp(user.ID, "operator-password", code, now); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
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

func TestHTTPAuthTotpSetupEnableDisableFlow(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	user, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "operator-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	meResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, want %d", meResponse.Code, http.StatusOK)
	}

	var mePayload struct {
		TotpEnabled bool `json:"totp_enabled"`
	}
	if err := json.Unmarshal(meResponse.Body.Bytes(), &mePayload); err != nil {
		t.Fatalf("json.Unmarshal(me) error = %v", err)
	}
	if mePayload.TotpEnabled {
		t.Fatal("me.totp_enabled = true, want false")
	}

	setupResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/totp/setup", nil, cookies)
	if setupResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/totp/setup status = %d, want %d", setupResponse.Code, http.StatusOK)
	}

	var setupPayload struct {
		Secret    string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(setupResponse.Body.Bytes(), &setupPayload); err != nil {
		t.Fatalf("json.Unmarshal(setup) error = %v", err)
	}
	if setupPayload.Secret == "" {
		t.Fatal("setup.secret = empty, want generated secret")
	}
	if setupPayload.OTPAuthURL == "" {
		t.Fatal("setup.otpauth_url = empty, want generated URL")
	}

	enableCode, err := server.auth.GenerateTotpCode(setupPayload.Secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode(enable) error = %v", err)
	}

	enableResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/totp/enable", map[string]string{
		"password":  "operator-password",
		"totp_code": enableCode,
	}, cookies)
	if enableResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/totp/enable status = %d, want %d", enableResponse.Code, http.StatusOK)
	}

	meEnabledResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meEnabledResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me after enable status = %d, want %d", meEnabledResponse.Code, http.StatusOK)
	}
	if err := json.Unmarshal(meEnabledResponse.Body.Bytes(), &mePayload); err != nil {
		t.Fatalf("json.Unmarshal(me enabled) error = %v", err)
	}
	if !mePayload.TotpEnabled {
		t.Fatal("me.totp_enabled = false after enable, want true")
	}

	logoutResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/logout", nil, cookies)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("POST /api/auth/logout status = %d, want %d", logoutResponse.Code, http.StatusNoContent)
	}

	loginWithoutTotp := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "operator-password",
	}, nil)
	if loginWithoutTotp.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/auth/login without totp status = %d, want %d", loginWithoutTotp.Code, http.StatusUnauthorized)
	}

	loginWithTotp := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "operator",
		"password":  "operator-password",
		"totp_code": enableCode,
	}, nil)
	if loginWithTotp.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login with totp status = %d, want %d", loginWithTotp.Code, http.StatusOK)
	}
	cookies = loginWithTotp.Result().Cookies()

	disableCode, err := server.auth.GenerateTotpCode(setupPayload.Secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode(disable) error = %v", err)
	}

	disableResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/totp/disable", map[string]string{
		"password":  "operator-password",
		"totp_code": disableCode,
	}, cookies)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/totp/disable status = %d, want %d", disableResponse.Code, http.StatusOK)
	}

	meDisabledResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meDisabledResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me after disable status = %d, want %d", meDisabledResponse.Code, http.StatusOK)
	}
	if err := json.Unmarshal(meDisabledResponse.Body.Bytes(), &mePayload); err != nil {
		t.Fatalf("json.Unmarshal(me disabled) error = %v", err)
	}
	if mePayload.TotpEnabled {
		t.Fatal("me.totp_enabled = true after disable, want false")
	}

	storedUser, err := server.auth.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if storedUser.TotpEnabled {
		t.Fatal("GetUserByID() TotpEnabled = true after disable, want false")
	}
}

func TestHTTPUsersTotpResetRequiresAdminAndClearsTarget(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 30, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	adminUser, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	operatorUser, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(operator) error = %v", err)
	}

	secret, err := server.auth.StartTotpSetup(operatorUser.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}
	code, err := server.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}
	if _, err := server.auth.EnableTotp(operatorUser.ID, "operator-password", code, now); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	viewerUser, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(viewer) error = %v", err)
	}

	viewerLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login viewer status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}

	viewerList := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/users", nil, viewerLogin.Result().Cookies())
	if viewerList.Code != http.StatusForbidden {
		t.Fatalf("GET /api/users as viewer status = %d, want %d", viewerList.Code, http.StatusForbidden)
	}

	adminLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login admin status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	adminCookies := adminLogin.Result().Cookies()

	usersResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/users", nil, adminCookies)
	if usersResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/users status = %d, want %d", usersResponse.Code, http.StatusOK)
	}

	var usersPayload []struct {
		ID          string `json:"id"`
		Role        string `json:"role"`
		TotpEnabled bool   `json:"totp_enabled"`
	}
	if err := json.Unmarshal(usersResponse.Body.Bytes(), &usersPayload); err != nil {
		t.Fatalf("json.Unmarshal(users) error = %v", err)
	}
	if len(usersPayload) != 3 {
		t.Fatalf("len(users) = %d, want %d", len(usersPayload), 3)
	}

	resetResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/users/"+operatorUser.ID+"/totp/reset", nil, adminCookies)
	if resetResponse.Code != http.StatusNoContent {
		t.Fatalf("POST /api/users/{id}/totp/reset status = %d, want %d", resetResponse.Code, http.StatusNoContent)
	}

	resetUser, err := server.auth.GetUserByID(operatorUser.ID)
	if err != nil {
		t.Fatalf("GetUserByID(reset target) error = %v", err)
	}
	if resetUser.TotpEnabled {
		t.Fatal("GetUserByID(reset target) TotpEnabled = true, want false")
	}

	operatorLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "operator-password",
	}, nil)
	if operatorLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login operator after reset status = %d, want %d", operatorLogin.Code, http.StatusOK)
	}

	auditResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/audit", nil, adminCookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit status = %d, want %d", auditResponse.Code, http.StatusOK)
	}

	var auditPayload []AuditEvent
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &auditPayload); err != nil {
		t.Fatalf("json.Unmarshal(audit) error = %v", err)
	}

	foundResetAudit := false
	for _, event := range auditPayload {
		if event.Action == "auth.totp.reset_by_admin" && event.ActorID == adminUser.ID && event.TargetID == operatorUser.ID {
			foundResetAudit = true
			break
		}
	}
	if !foundResetAudit {
		t.Fatalf("audit payload did not contain auth.totp.reset_by_admin for %s", operatorUser.ID)
	}

	if viewerUser.ID == "" {
		t.Fatal("viewer user id = empty, want seeded viewer record")
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
	user, _, err := bootstrap.BootstrapUser(auth.BootstrapInput{
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

	secret, err := first.auth.StartTotpSetup(user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}
	enableCode, err := first.auth.GenerateTotpCode(secret, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode(enable) error = %v", err)
	}
	if _, err := first.auth.EnableTotp(user.ID, "operator-password", enableCode, now.Add(10*time.Second)); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

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

func TestHTTPAgentBootstrapConsumesTokenAndReturnsIdentityBundle(t *testing.T) {
	now := time.Date(2026, time.March, 16, 14, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "default",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	bootstrapResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-a",
			"version":   "1.0.0",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
		},
	)
	if bootstrapResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/agent/bootstrap status = %d, want %d", bootstrapResponse.Code, http.StatusOK)
	}

	var payload struct {
		AgentID        string `json:"agent_id"`
		CertificatePEM string `json:"certificate_pem"`
		PrivateKeyPEM  string `json:"private_key_pem"`
		CAPEM          string `json:"ca_pem"`
		GRPCEndpoint   string `json:"grpc_endpoint"`
		GRPCServerName string `json:"grpc_server_name"`
	}
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(bootstrap) error = %v", err)
	}
	if payload.AgentID == "" {
		t.Fatal("bootstrap.agent_id = empty, want issued agent identity")
	}
	if payload.CertificatePEM == "" {
		t.Fatal("bootstrap.certificate_pem = empty, want issued certificate")
	}
	if payload.PrivateKeyPEM == "" {
		t.Fatal("bootstrap.private_key_pem = empty, want issued private key")
	}
	if payload.CAPEM == "" {
		t.Fatal("bootstrap.ca_pem = empty, want issued CA")
	}
	if payload.GRPCEndpoint != "panel.example.com:8443" {
		t.Fatalf("bootstrap.grpc_endpoint = %q, want %q", payload.GRPCEndpoint, "panel.example.com:8443")
	}
	if payload.GRPCServerName != "control-plane.panvex.internal" {
		t.Fatalf("bootstrap.grpc_server_name = %q, want %q", payload.GRPCServerName, "control-plane.panvex.internal")
	}

	storedToken, err := store.GetEnrollmentToken(context.Background(), token.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	if storedToken.ConsumedAt == nil {
		t.Fatal("GetEnrollmentToken() ConsumedAt = nil, want consumed token")
	}

	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(ListAgents()) = %d, want %d", len(agents), 1)
	}
	if agents[0].NodeName != "node-a" {
		t.Fatalf("ListAgents()[0].NodeName = %q, want %q", agents[0].NodeName, "node-a")
	}
}

func TestHTTPAgentBootstrapRejectsConsumedToken(t *testing.T) {
	now := time.Date(2026, time.March, 16, 14, 10, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "default",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	firstResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-a",
			"version":   "1.0.0",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
		},
	)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first POST /api/agent/bootstrap status = %d, want %d", firstResponse.Code, http.StatusOK)
	}

	secondResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-b",
			"version":   "1.0.1",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
		},
	)
	if secondResponse.Code != http.StatusForbidden {
		t.Fatalf("second POST /api/agent/bootstrap status = %d, want %d", secondResponse.Code, http.StatusForbidden)
	}

	var errorPayload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &errorPayload); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v", err)
	}
	if errorPayload.Error == "" {
		t.Fatal("bootstrap error payload = empty, want rejection reason")
	}

	storedToken, err := store.GetEnrollmentToken(context.Background(), token.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	if storedToken.ConsumedAt == nil {
		t.Fatal("GetEnrollmentToken() ConsumedAt = nil, want consumed token")
	}
}

func TestHTTPEnrollmentTokenListAndRevoke(t *testing.T) {
	now := time.Date(2026, time.March, 16, 14, 20, 0, 0, time.UTC)
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
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "operator-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/agents/enrollment-tokens", map[string]any{
		"environment_id": "prod",
		"fleet_group_id": "default",
		"ttl_seconds":    600,
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents/enrollment-tokens status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var createdToken struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &createdToken); err != nil {
		t.Fatalf("json.Unmarshal(create token) error = %v", err)
	}
	if createdToken.Value == "" {
		t.Fatal("created token value = empty, want active token")
	}

	listResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/agents/enrollment-tokens", nil, cookies)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/agents/enrollment-tokens status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	var listedTokens []struct {
		Value         string `json:"value"`
		EnvironmentID string `json:"environment_id"`
		FleetGroupID  string `json:"fleet_group_id"`
		Status        string `json:"status"`
		ExpiresAtUnix int64  `json:"expires_at_unix"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listedTokens); err != nil {
		t.Fatalf("json.Unmarshal(list tokens) error = %v", err)
	}
	if len(listedTokens) != 1 {
		t.Fatalf("len(tokens) = %d, want %d", len(listedTokens), 1)
	}
	if listedTokens[0].Value != createdToken.Value {
		t.Fatalf("tokens[0].value = %q, want %q", listedTokens[0].Value, createdToken.Value)
	}
	if listedTokens[0].Status != "active" {
		t.Fatalf("tokens[0].status = %q, want %q", listedTokens[0].Status, "active")
	}
	if listedTokens[0].EnvironmentID != "prod" {
		t.Fatalf("tokens[0].environment_id = %q, want %q", listedTokens[0].EnvironmentID, "prod")
	}
	if listedTokens[0].FleetGroupID != "default" {
		t.Fatalf("tokens[0].fleet_group_id = %q, want %q", listedTokens[0].FleetGroupID, "default")
	}
	if listedTokens[0].ExpiresAtUnix <= now.Unix() {
		t.Fatalf("tokens[0].expires_at_unix = %d, want future expiry", listedTokens[0].ExpiresAtUnix)
	}

	revokeResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/agents/enrollment-tokens/"+createdToken.Value+"/revoke", nil, cookies)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("POST /api/agents/enrollment-tokens/{value}/revoke status = %d, want %d", revokeResponse.Code, http.StatusNoContent)
	}

	listRevokedResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/agents/enrollment-tokens", nil, cookies)
	if listRevokedResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/agents/enrollment-tokens after revoke status = %d, want %d", listRevokedResponse.Code, http.StatusOK)
	}
	if err := json.Unmarshal(listRevokedResponse.Body.Bytes(), &listedTokens); err != nil {
		t.Fatalf("json.Unmarshal(list revoked tokens) error = %v", err)
	}
	if listedTokens[0].Status != "revoked" {
		t.Fatalf("tokens[0].status after revoke = %q, want %q", listedTokens[0].Status, "revoked")
	}

	bootstrapResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-a",
			"version":   "1.0.0",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + createdToken.Value,
		},
	)
	if bootstrapResponse.Code != http.StatusForbidden {
		t.Fatalf("POST /api/agent/bootstrap with revoked token status = %d, want %d", bootstrapResponse.Code, http.StatusForbidden)
	}

	storedToken, err := store.GetEnrollmentToken(context.Background(), createdToken.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	if storedToken.RevokedAt == nil {
		t.Fatal("GetEnrollmentToken() RevokedAt = nil, want revoked token")
	}
}

func TestHTTPControlRoomShowsFirstServerOnboarding(t *testing.T) {
	now := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
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

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	controlRoomResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/control-room", nil, loginResponse.Result().Cookies())
	if controlRoomResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/control-room status = %d, want %d", controlRoomResponse.Code, http.StatusOK)
	}

	var payload struct {
		Onboarding struct {
			NeedsFirstServer       bool   `json:"needs_first_server"`
			SetupComplete          bool   `json:"setup_complete"`
			SuggestedEnvironmentID string `json:"suggested_environment_id"`
			SuggestedFleetGroupID  string `json:"suggested_fleet_group_id"`
		} `json:"onboarding"`
		Fleet fleetResponse `json:"fleet"`
		Jobs  struct {
			Total   int `json:"total"`
			Queued  int `json:"queued"`
			Running int `json:"running"`
			Failed  int `json:"failed"`
		} `json:"jobs"`
		RecentActivity []AuditEvent `json:"recent_activity"`
	}
	if err := json.Unmarshal(controlRoomResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(control-room) error = %v", err)
	}

	if !payload.Onboarding.NeedsFirstServer {
		t.Fatal("onboarding.needs_first_server = false, want true")
	}
	if payload.Onboarding.SetupComplete {
		t.Fatal("onboarding.setup_complete = true, want false")
	}
	if payload.Onboarding.SuggestedEnvironmentID != "default" {
		t.Fatalf("onboarding.suggested_environment_id = %q, want %q", payload.Onboarding.SuggestedEnvironmentID, "default")
	}
	if payload.Onboarding.SuggestedFleetGroupID != "default" {
		t.Fatalf("onboarding.suggested_fleet_group_id = %q, want %q", payload.Onboarding.SuggestedFleetGroupID, "default")
	}
	if payload.Fleet.TotalAgents != 0 {
		t.Fatalf("fleet.total_agents = %d, want %d", payload.Fleet.TotalAgents, 0)
	}
	if payload.Jobs.Total != 0 || payload.Jobs.Queued != 0 || payload.Jobs.Running != 0 || payload.Jobs.Failed != 0 {
		t.Fatalf("jobs summary = %+v, want all zeros", payload.Jobs)
	}
	if len(payload.RecentActivity) != 0 {
		t.Fatalf("len(recent_activity) = %d, want %d", len(payload.RecentActivity), 0)
	}
}

func TestHTTPControlRoomSummarizesConnectedFleetAndActivity(t *testing.T) {
	currentTime := time.Date(2026, time.March, 16, 10, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return currentTime },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, currentTime); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	server.agents["agent-1"] = Agent{
		ID:            "agent-1",
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		LastSeenAt:    currentTime,
	}
	server.agents["agent-2"] = Agent{
		ID:            "agent-2",
		NodeName:      "node-b",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		LastSeenAt:    currentTime.Add(-45 * time.Second),
	}
	server.agents["agent-3"] = Agent{
		ID:            "agent-3",
		NodeName:      "node-c",
		EnvironmentID: "lab",
		FleetGroupID:  "edge",
		Version:       "1.0.0",
		LastSeenAt:    currentTime.Add(-2 * time.Minute),
	}
	server.instances["instance-1"] = Instance{
		ID:             "instance-1",
		AgentID:        "agent-1",
		Name:           "telemt-a",
		Version:        "1.0.0",
		ConnectedUsers: 27,
		UpdatedAt:      currentTime,
	}
	server.instances["instance-2"] = Instance{
		ID:             "instance-2",
		AgentID:        "agent-2",
		Name:           "telemt-b",
		Version:        "1.0.0",
		ConnectedUsers: 8,
		UpdatedAt:      currentTime.Add(-30 * time.Second),
	}
	server.presence.MarkConnected("agent-1", currentTime)
	server.presence.MarkConnected("agent-2", currentTime.Add(-45*time.Second))
	server.presence.MarkConnected("agent-3", currentTime.Add(-2*time.Minute))

	queuedJob, err := server.jobs.Enqueue(jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "control-room-queued",
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{"agent-1": false},
	}, currentTime.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(queued) error = %v", err)
	}
	runningJob, err := server.jobs.Enqueue(jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Minute,
		IdempotencyKey: "control-room-running",
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{"agent-2": false},
	}, currentTime.Add(-90*time.Second))
	if err != nil {
		t.Fatalf("Enqueue(running) error = %v", err)
	}
	server.jobs.MarkDelivered("agent-2", runningJob.ID, currentTime.Add(-80*time.Second))

	failedJob, err := server.jobs.Enqueue(jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-3"},
		TTL:            time.Minute,
		IdempotencyKey: "control-room-failed",
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{"agent-3": false},
	}, currentTime.Add(-70*time.Second))
	if err != nil {
		t.Fatalf("Enqueue(failed) error = %v", err)
	}
	server.jobs.RecordResult("agent-3", failedJob.ID, false, "connection lost", currentTime.Add(-60*time.Second))

	currentTime = currentTime.Add(-30 * time.Second)
	server.appendAudit("user-1", "agents.enrollment.create", "token-1", map[string]any{
		"environment_id": "prod",
	})
	currentTime = currentTime.Add(10 * time.Second)
	server.appendAudit("user-1", "jobs.create", queuedJob.ID, map[string]any{
		"action": string(queuedJob.Action),
	})
	currentTime = currentTime.Add(20 * time.Second)

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	controlRoomResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/control-room", nil, loginResponse.Result().Cookies())
	if controlRoomResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/control-room status = %d, want %d", controlRoomResponse.Code, http.StatusOK)
	}

	var payload struct {
		Onboarding struct {
			NeedsFirstServer       bool   `json:"needs_first_server"`
			SetupComplete          bool   `json:"setup_complete"`
			SuggestedEnvironmentID string `json:"suggested_environment_id"`
			SuggestedFleetGroupID  string `json:"suggested_fleet_group_id"`
		} `json:"onboarding"`
		Fleet fleetResponse `json:"fleet"`
		Jobs  struct {
			Total   int `json:"total"`
			Queued  int `json:"queued"`
			Running int `json:"running"`
			Failed  int `json:"failed"`
		} `json:"jobs"`
		RecentActivity []AuditEvent `json:"recent_activity"`
	}
	if err := json.Unmarshal(controlRoomResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(control-room) error = %v", err)
	}

	if payload.Onboarding.NeedsFirstServer {
		t.Fatal("onboarding.needs_first_server = true, want false")
	}
	if !payload.Onboarding.SetupComplete {
		t.Fatal("onboarding.setup_complete = false, want true")
	}
	if payload.Onboarding.SuggestedEnvironmentID != "prod" {
		t.Fatalf("onboarding.suggested_environment_id = %q, want %q", payload.Onboarding.SuggestedEnvironmentID, "prod")
	}
	if payload.Onboarding.SuggestedFleetGroupID != "ams-1" {
		t.Fatalf("onboarding.suggested_fleet_group_id = %q, want %q", payload.Onboarding.SuggestedFleetGroupID, "ams-1")
	}
	if payload.Fleet.TotalAgents != 3 {
		t.Fatalf("fleet.total_agents = %d, want %d", payload.Fleet.TotalAgents, 3)
	}
	if payload.Fleet.OnlineAgents != 1 {
		t.Fatalf("fleet.online_agents = %d, want %d", payload.Fleet.OnlineAgents, 1)
	}
	if payload.Fleet.DegradedAgents != 1 {
		t.Fatalf("fleet.degraded_agents = %d, want %d", payload.Fleet.DegradedAgents, 1)
	}
	if payload.Fleet.OfflineAgents != 1 {
		t.Fatalf("fleet.offline_agents = %d, want %d", payload.Fleet.OfflineAgents, 1)
	}
	if payload.Fleet.TotalInstances != 2 {
		t.Fatalf("fleet.total_instances = %d, want %d", payload.Fleet.TotalInstances, 2)
	}
	if payload.Jobs.Total != 3 {
		t.Fatalf("jobs.total = %d, want %d", payload.Jobs.Total, 3)
	}
	if payload.Jobs.Queued != 1 {
		t.Fatalf("jobs.queued = %d, want %d", payload.Jobs.Queued, 1)
	}
	if payload.Jobs.Running != 1 {
		t.Fatalf("jobs.running = %d, want %d", payload.Jobs.Running, 1)
	}
	if payload.Jobs.Failed != 1 {
		t.Fatalf("jobs.failed = %d, want %d", payload.Jobs.Failed, 1)
	}
	if len(payload.RecentActivity) != 2 {
		t.Fatalf("len(recent_activity) = %d, want %d", len(payload.RecentActivity), 2)
	}
	if payload.RecentActivity[0].Action != "jobs.create" {
		t.Fatalf("recent_activity[0].action = %q, want %q", payload.RecentActivity[0].Action, "jobs.create")
	}
	if payload.RecentActivity[1].Action != "agents.enrollment.create" {
		t.Fatalf("recent_activity[1].action = %q, want %q", payload.RecentActivity[1].Action, "agents.enrollment.create")
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

	return performJSONRequestWithHeaders(t, handler, method, path, body, cookies, nil)
}

func performJSONRequestWithHeaders(t *testing.T, handler http.Handler, method string, path string, body any, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
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
	for key, value := range headers {
		request.Header.Set(key, value)
	}
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
