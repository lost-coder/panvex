package telemt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contract-audit F1: the per-user quota list lives at Telemt's
// /v1/stats/users/quota (mod.rs "GET /v1/stats/users/quota"). The agent
// used to call /v1/users/quota, which falls into the /v1/users/{name}
// catch-all as a lookup for a user literally named "quota" and 404s on
// every Telemt version — silently swallowed by the old-Telemt tolerance,
// so QuotaUsedBytes/QuotaLastResetUnix never populated in production.

// quotaTestServer serves the REAL Telemt quota route and flags any
// request to the phantom /v1/users/quota path.
func quotaTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/stats/users/quota":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"users":[{"username":"alice","data_quota_bytes":1024,"used_bytes":512,"last_reset_epoch_secs":1700000000}]}}`))
		case "/v1/users/quota":
			t.Errorf("agent requested the phantom /v1/users/quota path (hits the user-by-name catch-all on real Telemt)")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"User not found"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Route not found"}}`))
		}
	}))
}

func TestFetchUserQuotaInfoUsesStatsRoute(t *testing.T) {
	srv := quotaTestServer(t)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	quotas, err := client.fetchUserQuotaInfo(context.Background())
	if err != nil {
		t.Fatalf("fetchUserQuotaInfo() error = %v", err)
	}
	if len(quotas) != 1 {
		t.Fatalf("len(quotas) = %d, want 1 (quota data must come from /v1/stats/users/quota)", len(quotas))
	}
	q := quotas[0]
	if q.Username != "alice" || q.DataQuotaBytes != 1024 || q.UsedBytes != 512 || q.LastResetEpochSecs != 1700000000 {
		t.Fatalf("quota row = %+v, want alice/1024/512/1700000000", q)
	}
}

func TestFetchUserQuotaInfoTolerates404(t *testing.T) {
	// Telemt < 3.4.12 has no stats/users/quota route at all — the agent
	// must treat that 404 as "no quota data", not a hard error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Route not found"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	quotas, err := client.fetchUserQuotaInfo(context.Background())
	if err != nil {
		t.Fatalf("fetchUserQuotaInfo() on 404 must be tolerated, got error = %v", err)
	}
	if len(quotas) != 0 {
		t.Fatalf("len(quotas) = %d, want 0 on 404", len(quotas))
	}
}
