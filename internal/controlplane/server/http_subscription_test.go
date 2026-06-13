package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// seedSubscriptionClient creates a client via createClient (always enabled,
// no expiry by default) using the fleet-group from newSubscriptionTestServer,
// then optionally patches it with updateClient to apply the given
// clientMutationInput overrides. Returns the client's SubscriptionToken.
func seedSubscriptionClient(
	t *testing.T,
	server *Server,
	groupID string,
	name string,
	now time.Time,
	patch *clientMutationInput, // nil → leave as-is (enabled, no expiry)
) string {
	t.Helper()
	ctx := context.Background()

	created, _, _, err := server.createClient(ctx, "user-sub-test", clientMutationInput{
		Name:          name,
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("seedSubscriptionClient createClient(%q): %v", name, err)
	}
	if created.SubscriptionToken == "" {
		t.Fatalf("seedSubscriptionClient: SubscriptionToken empty after create")
	}

	if patch != nil {
		// Carry over required fields that updateClient needs.
		if patch.Name == "" {
			patch.Name = name
		}
		if len(patch.FleetGroupIDs) == 0 {
			patch.FleetGroupIDs = []string{groupID}
		}
		if _, _, _, err := server.updateClient(ctx, string(created.ID), "user-sub-test", *patch, now); err != nil {
			t.Fatalf("seedSubscriptionClient updateClient(%q): %v", name, err)
		}
	}

	return created.SubscriptionToken
}

// mountSubRouter returns an httptest.Server with only GET /sub/{token} wired.
func mountSubRouter(srv *Server) *httptest.Server {
	r := chi.NewRouter()
	r.Get("/sub/{token}", srv.handleSubscriptionPage())
	return httptest.NewServer(r)
}

// ---------------------------------------------------------------------------
// HTTP integration tests
// ---------------------------------------------------------------------------

func TestHandleSubscriptionPage_OK(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)

	token := seedSubscriptionClient(t, server, groupID, "alice-ok", now, nil)

	ts := mountSubRouter(server)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sub/" + token)
	if err != nil {
		t.Fatalf("GET /sub/{token}: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "alice-ok") {
		t.Errorf("body does not contain client name %q; got: %s", "alice-ok", body)
	}
}

func TestHandleSubscriptionPage_MissingToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, _ := newSubscriptionTestServer(t, now)

	ts := mountSubRouter(server)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sub/tok-missing-xxxxxxxxxxx")
	if err != nil {
		t.Fatalf("GET /sub/tok-missing: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Подписка неактивна") {
		t.Errorf("body does not contain inactive message; got: %s", body)
	}
}

func TestHandleSubscriptionPage_Disabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)

	disabled := false
	token := seedSubscriptionClient(t, server, groupID, "bob-disabled", now, &clientMutationInput{
		Enabled: &disabled,
	})

	ts := mountSubRouter(server)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sub/" + token)
	if err != nil {
		t.Fatalf("GET /sub/{token}: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", resp.StatusCode, body)
	}
}

func TestHandleSubscriptionPage_Expired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)

	// Expiration one hour in the past relative to the server's now.
	pastExpiry := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	token := seedSubscriptionClient(t, server, groupID, "carol-expired", now, &clientMutationInput{
		ExpirationRFC3339: pastExpiry,
	})

	ts := mountSubRouter(server)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sub/" + token)
	if err != nil {
		t.Fatalf("GET /sub/{token}: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", resp.StatusCode, body)
	}
}

func TestHandleSubscriptionPage_XRobotsTag(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, _ := newSubscriptionTestServer(t, now)

	ts := mountSubRouter(server)
	defer ts.Close()

	// Even a missing-token response must carry X-Robots-Tag.
	resp, err := http.Get(ts.URL + "/sub/tok-robots-check")
	if err != nil {
		t.Fatalf("GET /sub/tok-robots-check: %v", err)
	}
	resp.Body.Close()

	if got := resp.Header.Get("X-Robots-Tag"); got == "" {
		t.Error("X-Robots-Tag header missing")
	}
}

// ---------------------------------------------------------------------------
// Unit tests for subscriptionClientActive
// ---------------------------------------------------------------------------

func TestSubscriptionClientActive(t *testing.T) {
	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	past := now.Add(-time.Hour).UTC().Format(time.RFC3339)

	cases := []struct {
		name   string
		client clients.Client
		want   bool
	}{
		{
			name:   "enabled, no expiry",
			client: clients.Client{Enabled: true, ExpirationRFC3339: ""},
			want:   true,
		},
		{
			name:   "disabled",
			client: clients.Client{Enabled: false, ExpirationRFC3339: ""},
			want:   false,
		},
		{
			name:   "enabled, future expiry",
			client: clients.Client{Enabled: true, ExpirationRFC3339: future},
			want:   true,
		},
		{
			name:   "enabled, past expiry",
			client: clients.Client{Enabled: true, ExpirationRFC3339: past},
			want:   false,
		},
		{
			name:   "enabled, blank expiry treated as no expiry",
			client: clients.Client{Enabled: true, ExpirationRFC3339: ""},
			want:   true,
		},
		{
			name:   "enabled, malformed expiry treated as no expiry",
			client: clients.Client{Enabled: true, ExpirationRFC3339: "not-a-date"},
			want:   true,
		},
		{
			name:   "disabled overrides future expiry",
			client: clients.Client{Enabled: false, ExpirationRFC3339: future},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subscriptionClientActive(tc.client, now)
			if got != tc.want {
				t.Errorf("subscriptionClientActive() = %v, want %v", got, tc.want)
			}
		})
	}
}
