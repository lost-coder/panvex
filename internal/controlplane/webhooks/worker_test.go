package webhooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerHappyPath(t *testing.T) {
	t.Setenv(EnvAllowInsecureWebhook, "1")

	var hits atomic.Int32
	var lastSig, lastTimestamp string
	var lastBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastSig = r.Header.Get("X-Panvex-Signature")
		lastTimestamp = r.Header.Get("X-Panvex-Timestamp")
		lastBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMemStore()
	secret := []byte("hunter2")
	store.addEndpoint(Endpoint{
		ID: "ep-1", Name: "test", URL: srv.URL, Secret: secret,
		AllowPrivate: true, // httptest uses 127.0.0.1
		Enabled:      true,
	})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	row := OutboxRow{
		ID: "deliv-1", EndpointID: "ep-1",
		EventAction:   "agent.unhealthy",
		Payload:       json.RawMessage(`{"agent":"a-1"}`),
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := store.InsertOutbox(context.Background(), row); err != nil {
		t.Fatal(err)
	}

	w := NewWorker(store, WorkerConfig{
		Clock: func() time.Time { return now },
	})
	w.tick(context.Background())

	if hits.Load() != 1 {
		t.Fatalf("receiver hits = %d, want 1", hits.Load())
	}
	got, ok := store.snapshot("deliv-1")
	if !ok {
		t.Fatal("row missing after delivery")
	}
	if got.DeliveredAt == nil {
		t.Errorf("DeliveredAt nil; expected non-nil after 200")
	}
	if got.Dead {
		t.Errorf("row marked dead after success")
	}

	// Signature must verify against the body the receiver actually saw.
	if !Verify(secret, []byte(lastTimestamp), lastBody, lastSig) {
		t.Errorf("signature verify failed: ts=%q sig=%q body=%q", lastTimestamp, lastSig, lastBody)
	}
}

func TestWorkerRetriesOn5xx(t *testing.T) {
	t.Setenv(EnvAllowInsecureWebhook, "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := newMemStore()
	store.addEndpoint(Endpoint{
		ID: "ep-1", URL: srv.URL, Secret: []byte("k"),
		AllowPrivate: true, Enabled: true,
	})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := store.InsertOutbox(context.Background(), OutboxRow{
		ID: "r1", EndpointID: "ep-1",
		EventAction: "x.y", Payload: json.RawMessage(`{}`),
		NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InsertOutbox: %v", err)
	}

	w := NewWorker(store, WorkerConfig{
		Clock:   func() time.Time { return now },
		Backoff: func(attempt int) time.Duration { return time.Duration(attempt) * time.Second },
	})
	w.tick(context.Background())

	got, _ := store.snapshot("r1")
	if got.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1 after one 5xx", got.Attempt)
	}
	if got.Dead {
		t.Errorf("Dead = true after a single failure (MaxAttempts default is 8)")
	}
	if got.DeliveredAt != nil {
		t.Errorf("DeliveredAt set after 5xx")
	}
	if got.LastError == "" {
		t.Errorf("LastError empty after 5xx")
	}
	wantNext := now.Add(time.Second)
	if !got.NextAttemptAt.Equal(wantNext) {
		t.Errorf("NextAttemptAt = %v, want %v (now + backoff(1)=1s)", got.NextAttemptAt, wantNext)
	}
}

func TestWorkerDeadLettersAfterMaxAttempts(t *testing.T) {
	t.Setenv(EnvAllowInsecureWebhook, "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	store := newMemStore()
	store.addEndpoint(Endpoint{
		ID: "ep-1", URL: srv.URL, Secret: []byte("k"),
		AllowPrivate: true, Enabled: true,
	})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := store.InsertOutbox(context.Background(), OutboxRow{
		ID: "r1", EndpointID: "ep-1",
		EventAction: "x.y", Payload: json.RawMessage(`{}`),
		Attempt:       2, // one tick away from MaxAttempts=3 below
		NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InsertOutbox: %v", err)
	}

	var deadLetters []DeadLetter
	w := NewWorker(store, WorkerConfig{
		MaxAttempts: 3,
		Clock:       func() time.Time { return now },
		Backoff:     func(int) time.Duration { return time.Second },
		OnDeadLetter: func(_ context.Context, dl DeadLetter) {
			deadLetters = append(deadLetters, dl)
		},
	})
	w.tick(context.Background())

	got, _ := store.snapshot("r1")
	if got.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3 (MaxAttempts boundary)", got.Attempt)
	}
	if !got.Dead {
		t.Errorf("Dead = false; expected dead-letter once Attempt reaches MaxAttempts")
	}
	// R4 §1.7: the dead-letter must be surfaced durably via the hook, not only logged.
	if len(deadLetters) != 1 {
		t.Fatalf("OnDeadLetter calls = %d, want 1", len(deadLetters))
	}
	if deadLetters[0].OutboxID != "r1" || deadLetters[0].Attempts != 3 {
		t.Fatalf("dead-letter = %+v, want OutboxID=r1 Attempts=3", deadLetters[0])
	}
}

func TestWorkerPreflightRejectsHTTPInProd(t *testing.T) {
	// Do NOT set EnvAllowInsecureWebhook — http:// must be refused.
	store := newMemStore()
	store.addEndpoint(Endpoint{
		ID: "ep-1", URL: "http://example.com/hook", Secret: []byte("k"),
		AllowPrivate: true, Enabled: true,
	})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := store.InsertOutbox(context.Background(), OutboxRow{
		ID: "r1", EndpointID: "ep-1",
		EventAction: "x.y", Payload: json.RawMessage(`{}`),
		NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InsertOutbox: %v", err)
	}
	w := NewWorker(store, WorkerConfig{
		MaxAttempts: 5,
		Clock:       func() time.Time { return now },
	})
	w.tick(context.Background())

	got, _ := store.snapshot("r1")
	if !got.Dead {
		t.Errorf("Dead = false; preflight should permanently fail an http:// URL when insecure not allowed")
	}
}

func TestWorkerPreflightRejectsPrivateCIDRWithoutOptIn(t *testing.T) {
	store := newMemStore()
	// 127.0.0.1 is loopback — private. AllowPrivate is false.
	store.addEndpoint(Endpoint{
		ID: "ep-1", URL: "https://127.0.0.1/hook", Secret: []byte("k"),
		AllowPrivate: false, Enabled: true,
	})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := store.InsertOutbox(context.Background(), OutboxRow{
		ID: "r1", EndpointID: "ep-1",
		EventAction: "x.y", Payload: json.RawMessage(`{}`),
		NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InsertOutbox: %v", err)
	}
	w := NewWorker(store, WorkerConfig{
		MaxAttempts: 5,
		Clock:       func() time.Time { return now },
	})
	w.tick(context.Background())

	got, _ := store.snapshot("r1")
	if !got.Dead {
		t.Errorf("private-CIDR URL should be dead-lettered without allow_private=true")
	}
}

func TestExponentialBackoffCap(t *testing.T) {
	cases := []struct {
		attempt int
		minD    time.Duration
		maxD    time.Duration
	}{
		{1, 30 * time.Second, 30 * time.Second},
		{2, 60 * time.Second, 60 * time.Second},
		{3, 120 * time.Second, 120 * time.Second},
		{8, time.Hour, time.Hour},  // capped
		{20, time.Hour, time.Hour}, // far past cap
	}
	for _, c := range cases {
		t.Run("attempt="+strconv.Itoa(c.attempt), func(t *testing.T) {
			d := exponentialBackoff(c.attempt)
			if d < c.minD || d > c.maxD {
				t.Errorf("backoff(%d) = %v, want in [%v, %v]", c.attempt, d, c.minD, c.maxD)
			}
		})
	}
}

// TestWorkerGuardsEgressForNonPrivateEndpoints checks the delivery client
// selection: an endpoint without allow_private goes through the egress-guarded
// client, which refuses a private destination at dial time even if preflight's
// resolve-time check were bypassed (DNS rebinding). An allow_private endpoint
// keeps the plain client so operators can reach an internal receiver.
func TestWorkerGuardsEgressForNonPrivateEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	worker := NewWorker(newMemStore(), WorkerConfig{HTTPClient: srv.Client()})

	private := Endpoint{AllowPrivate: true}
	if worker.clientFor(private) != worker.cfg.HTTPClient {
		t.Fatal("allow_private endpoint must use the plain client")
	}

	public := Endpoint{AllowPrivate: false}
	guarded := worker.clientFor(public)
	if guarded == worker.cfg.HTTPClient {
		t.Fatal("endpoint without allow_private must use the guarded client")
	}

	// The guarded client must refuse the loopback httptest server.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := guarded.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("guarded client reached a loopback address; want dial refused")
	}
}
