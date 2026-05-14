package webhooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// EnvAllowInsecureWebhook opts into http:// receiver URLs (in
// addition to the default https-only). Dev fixtures pointing at a
// local mock receiver set this; prod must not.
const EnvAllowInsecureWebhook = "PANVEX_ALLOW_INSECURE_WEBHOOK"

// WorkerConfig configures the delivery loop. Zero / unset fields
// fall back to defaults documented per field.
type WorkerConfig struct {
	// Interval between claim cycles. Default: 5s.
	Interval time.Duration
	// MaxAttempts before a row is dead-lettered. Default: 8 (≈4h
	// total wall-time at the default backoff).
	MaxAttempts int
	// BatchSize is the maximum rows claimed per cycle. Default: 32.
	BatchSize int
	// Backoff returns the delay before the next attempt. Defaults
	// to exponential 2^attempt * 30s capped at 1h.
	Backoff func(attempt int) time.Duration
	// HTTPClient drives the actual POST. A nil value uses a 10s
	// timeout client.
	HTTPClient *http.Client
	// Clock — overridable for tests.
	Clock func() time.Time
	// Logger — defaults to slog.Default().
	Logger *slog.Logger
}

// Worker delivers queued outbox rows. Run blocks until ctx is
// cancelled.
type Worker struct {
	storage Storage
	cfg     WorkerConfig
}

// NewWorker fills in defaults on cfg and returns a ready Worker.
func NewWorker(storage Storage, cfg WorkerConfig) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.Backoff == nil {
		cfg.Backoff = exponentialBackoff
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Worker{storage: storage, cfg: cfg}
}

// Run drives the delivery loop until ctx.Done(). Errors from the
// storage / HTTP layers are logged and retried — the loop never
// returns until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	// First tick immediately so a single Publish during a unit test
	// doesn't have to wait Interval before draining.
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// Tick is exposed for tests that drive the worker manually
// (avoiding goroutine + ticker indirection in unit fixtures).
func (w *Worker) Tick(ctx context.Context) { w.tick(ctx) }

func (w *Worker) tick(ctx context.Context) {
	now := w.cfg.Clock().UTC()
	start := time.Now()
	deliveries, err := w.storage.ClaimReady(ctx, now, w.cfg.BatchSize)
	if err != nil {
		w.cfg.Logger.WarnContext(ctx, "webhooks: claim ready",
			"worker", "webhook_outbox",
			"lap_ms", time.Since(start).Milliseconds(),
			"error", err)
		return
	}
	for _, d := range deliveries {
		w.deliver(ctx, d)
	}
	elapsed := time.Since(start)
	// Per-tick lap log (P2-LOG-10 / L-10). Idle ticks stay at Debug so
	// production stays quiet; non-idle ticks go to Info so operators can
	// see the outbox draining.
	if len(deliveries) == 0 {
		w.cfg.Logger.DebugContext(ctx, "rollup worker tick idle",
			"worker", "webhook_outbox",
			"lap_ms", elapsed.Milliseconds())
		return
	}
	w.cfg.Logger.InfoContext(ctx, "rollup worker tick",
		"worker", "webhook_outbox",
		"lap_ms", elapsed.Milliseconds(),
		"rows_affected", len(deliveries))
}

func (w *Worker) deliver(ctx context.Context, d Delivery) {
	if err := preflight(ctx, d.Endpoint); err != nil {
		w.failPermanently(ctx, d, fmt.Errorf("preflight: %w", err))
		return
	}
	now := w.cfg.Clock().UTC()
	timestamp := []byte(now.Format(time.RFC3339Nano))
	body := []byte(d.Outbox.Payload)
	signature := Sign(d.Endpoint.Secret, timestamp, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint.URL, bytes.NewReader(body))
	if err != nil {
		w.recordFailure(ctx, d, fmt.Errorf("build request: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Panvex-Event", d.Outbox.EventAction)
	req.Header.Set("X-Panvex-Delivery", d.Outbox.ID)
	req.Header.Set("X-Panvex-Timestamp", string(timestamp))
	req.Header.Set("X-Panvex-Signature", signature)

	resp, err := w.cfg.HTTPClient.Do(req)
	if err != nil {
		w.recordFailure(ctx, d, fmt.Errorf("post: %w", err))
		return
	}
	defer func() {
		// best-effort drain + close: ignoring errors is intentional
		// (connection re-use only, no caller depends on either).
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := w.storage.MarkDelivered(ctx, d.Outbox.ID, now); err != nil {
			w.cfg.Logger.Warn("webhooks: mark delivered", "id", d.Outbox.ID, "error", err)
		}
		return
	}
	w.recordFailure(ctx, d, fmt.Errorf("receiver returned %d", resp.StatusCode))
}

// recordFailure increments attempt and reschedules, or dead-letters
// if MaxAttempts exhausted.
func (w *Worker) recordFailure(ctx context.Context, d Delivery, deliverErr error) {
	attempt := d.Outbox.Attempt + 1
	dead := attempt >= w.cfg.MaxAttempts
	next := w.cfg.Clock().UTC().Add(w.cfg.Backoff(attempt))
	errMsg := truncateError(deliverErr.Error())
	if err := w.storage.MarkFailed(ctx, d.Outbox.ID, attempt, next, errMsg, dead); err != nil {
		w.cfg.Logger.Warn("webhooks: mark failed", "id", d.Outbox.ID, "error", err)
		return
	}
	if dead {
		w.cfg.Logger.Error("webhooks: dead-letter",
			"id", d.Outbox.ID,
			"endpoint", d.Endpoint.Name,
			"action", d.Outbox.EventAction,
			"attempts", attempt,
			"last_error", errMsg,
		)
	}
}

// failPermanently dead-letters a row that cannot be delivered ever
// (preflight refused — not an https URL, private CIDR without
// allow_private, …). Increments attempt to the cap so a future
// loosened preflight doesn't accidentally retry the same row.
func (w *Worker) failPermanently(ctx context.Context, d Delivery, deliverErr error) {
	next := w.cfg.Clock().UTC().Add(w.cfg.Backoff(w.cfg.MaxAttempts))
	if err := w.storage.MarkFailed(ctx, d.Outbox.ID, w.cfg.MaxAttempts, next, truncateError(deliverErr.Error()), true); err != nil {
		w.cfg.Logger.Warn("webhooks: mark permanently failed", "id", d.Outbox.ID, "error", err)
		return
	}
	w.cfg.Logger.Error("webhooks: dead-letter (preflight)",
		"id", d.Outbox.ID,
		"endpoint", d.Endpoint.Name,
		"error", deliverErr,
	)
}

// preflight rejects URLs the worker is not allowed to dial. Run
// before each attempt because endpoint config can change between
// publish and delivery (operator edits the URL after a row is
// already in the outbox).
func preflight(ctx context.Context, ep Endpoint) error {
	u, err := url.Parse(ep.URL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	switch u.Scheme {
	case "https":
		// always ok
	case "http":
		if !insecureAllowed() {
			return fmt.Errorf("webhook URL scheme http is not allowed (set %s=1 for dev)", EnvAllowInsecureWebhook)
		}
	default:
		return fmt.Errorf("webhook URL scheme %q is not http(s)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook URL has no host")
	}
	if !ep.AllowPrivate {
		if isPrivateHost(ctx, host) {
			return fmt.Errorf("webhook URL %q resolves to private/loopback CIDR; set allow_private=true to override", host)
		}
	}
	return nil
}

func insecureAllowed() bool {
	return strings.TrimSpace(os.Getenv(EnvAllowInsecureWebhook)) == "1"
}

// isPrivateHost is a best-effort SSRF guard. Resolves the host and
// flags any address in private / loopback / link-local space. The
// real defence-in-depth is the AllowPrivate flag — this function
// only catches the obvious cases without making the dev experience
// painful (operators are expected to override deliberately).
//
// Uses the context-aware resolver so a wedged DNS lookup can be
// reaped by the worker's tick / shutdown context (noctx lint).
func isPrivateHost(ctx context.Context, host string) bool {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// Fail closed: if we can't resolve, treat as private.
		return true
	}
	for _, a := range addrs {
		ip := a.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

// exponentialBackoff: 2^attempt * 30s, capped at 1h. Attempt is
// 1-indexed (first failure → 1).
func exponentialBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	const base = 30 * time.Second
	const cap = time.Hour
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= cap {
			return cap
		}
	}
	return d
}

// truncateError keeps last_error short enough not to bloat the row
// (and cheap to log).
func truncateError(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
