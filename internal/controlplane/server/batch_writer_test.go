package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestBatchBufferDrainFlushesAccumulatedItems(t *testing.T) {
	var flushed []int
	buf := newBatchBuffer(10, func(_ context.Context, items []int) {
		flushed = append(flushed, items...)
	})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Enqueue(3)
	buf.Drain(context.Background())

	if len(flushed) != 3 {
		t.Fatalf("flushed %d items, want 3", len(flushed))
	}
	for i, want := range []int{1, 2, 3} {
		if flushed[i] != want {
			t.Fatalf("flushed[%d] = %d, want %d", i, flushed[i], want)
		}
	}
}

func TestBatchBufferDrainIsNoOpWhenEmpty(t *testing.T) {
	called := false
	buf := newBatchBuffer(10, func(_ context.Context, _ []int) {
		called = true
	})

	buf.Drain(context.Background())

	if called {
		t.Fatal("flush function was called on empty buffer")
	}
}

func TestBatchBufferSignalFiringOnFull(t *testing.T) {
	buf := newBatchBuffer(3, func(_ context.Context, _ []int) {})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Enqueue(3)

	select {
	case <-buf.signal:
		// expected
	default:
		t.Fatal("signal channel should have a message after buffer reaches maxSize")
	}
}

func TestBatchBufferDrainResetsBuffer(t *testing.T) {
	var mu sync.Mutex
	var batches [][]int

	buf := newBatchBuffer(10, func(_ context.Context, items []int) {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]int, len(items))
		copy(cp, items)
		batches = append(batches, cp)
	})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Drain(context.Background())

	buf.Enqueue(3)
	buf.Drain(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("first batch has %d items, want 2", len(batches[0]))
	}
	if len(batches[1]) != 1 {
		t.Fatalf("second batch has %d items, want 1", len(batches[1]))
	}
	if batches[1][0] != 3 {
		t.Fatalf("second batch[0] = %d, want 3", batches[1][0])
	}
}

func TestStoreBatchWriterStartAndStop(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	w := newStoreBatchWriter(store, nil)
	w.Start()
	w.Stop()
}

// -----------------------------------------------------------------------
// P2-REL-06: batch_writer error classification + retry + metrics tests.
// -----------------------------------------------------------------------

// recordingSink is a minimal batchMetricsSink that tallies every observation
// so tests can assert on per-stream/per-type counts without wiring a real
// Prometheus registry.
type recordingSink struct {
	mu             sync.Mutex
	flushErrors    map[string]int // key = buffer|errorType
	persistRetries map[string]int // key = stream|outcome
	depths         map[string]float64
	// P2-OBS-03: per-flush duration observations, stream-labelled. Appended in
	// the order the batch writer recorded them so tests can assert both count
	// and ordering.
	observeCalls []struct {
		stream  string
		seconds float64
	}
}

func newRecordingSink() *recordingSink {
	return &recordingSink{
		flushErrors:    map[string]int{},
		persistRetries: map[string]int{},
		depths:         map[string]float64{},
	}
}

func (s *recordingSink) ObserveFlushError(buffer, errorType string) {
	s.mu.Lock()
	s.flushErrors[buffer+"|"+errorType]++
	s.mu.Unlock()
}

func (s *recordingSink) SetQueueDepth(buffer string, depth float64) {
	s.mu.Lock()
	s.depths[buffer] = depth
	s.mu.Unlock()
}

func (s *recordingSink) ObservePersistRetry(stream, outcome string) {
	s.mu.Lock()
	s.persistRetries[stream+"|"+outcome]++
	s.mu.Unlock()
}

func (s *recordingSink) ObserveFlushDuration(stream string, seconds float64) {
	s.mu.Lock()
	s.observeCalls = append(s.observeCalls, struct {
		stream  string
		seconds float64
	}{stream: stream, seconds: seconds})
	s.mu.Unlock()
}

func (s *recordingSink) durationCount(stream string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.observeCalls {
		if c.stream == stream {
			n++
		}
	}
	return n
}

func (s *recordingSink) flushErrorCount(buffer, errorType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushErrors[buffer+"|"+errorType]
}

func (s *recordingSink) retryCount(stream, outcome string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistRetries[stream+"|"+outcome]
}

// newTestBatchWriter builds a storeBatchWriter wired up for fast, deterministic
// unit tests: real sleep calls are replaced with a no-op so the full retry
// schedule completes in microseconds, and the passed metrics sink captures
// every observation.
func newTestBatchWriter(store storage.Store, sink *recordingSink) *storeBatchWriter {
	w := newStoreBatchWriter(store, sink)
	w.sleep = func(time.Duration) {} // collapse the backoff to zero
	return w
}

// sequencedFailStore is a storage.Store wrapper whose PutAgent /
// AppendMetricSnapshot / AppendDCHealthPoint functions fail for the first N
// calls with a caller-provided error, then succeed (delegating to the
// embedded store). The call count is tracked per-method so tests can set up
// independent failure budgets for different streams.
type sequencedFailStore struct {
	storage.Store

	putAgentFailFn func(attempt int) error
	putAgentCalls  atomic.Int32

	appendMetricFailFn func(attempt int) error
	appendMetricCalls  atomic.Int32

	appendDCFailFn func(attempt int) error
	appendDCCalls  atomic.Int32
}

func (s *sequencedFailStore) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	n := int(s.putAgentCalls.Add(1))
	if s.putAgentFailFn != nil {
		if err := s.putAgentFailFn(n); err != nil {
			return err
		}
	}
	return s.Store.PutAgent(ctx, agent)
}

func (s *sequencedFailStore) AppendMetricSnapshot(ctx context.Context, snap storage.MetricSnapshotRecord) error {
	n := int(s.appendMetricCalls.Add(1))
	if s.appendMetricFailFn != nil {
		if err := s.appendMetricFailFn(n); err != nil {
			return err
		}
	}
	return s.Store.AppendMetricSnapshot(ctx, snap)
}

func (s *sequencedFailStore) AppendDCHealthPoint(ctx context.Context, p storage.DCHealthPointRecord) error {
	n := int(s.appendDCCalls.Add(1))
	if s.appendDCFailFn != nil {
		if err := s.appendDCFailFn(n); err != nil {
			return err
		}
	}
	return s.Store.AppendDCHealthPoint(ctx, p)
}

func newSequencedStore(t *testing.T) *sequencedFailStore {
	t.Helper()
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	return &sequencedFailStore{Store: base}
}

func TestClassifyFlushError_Transient(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"context canceled", context.Canceled},
		{"context deadline", context.DeadlineExceeded},
		{"connection refused", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")},
		{"connection reset", errors.New("read tcp: connection reset by peer")},
		{"i/o timeout", errors.New("net/http: i/o timeout")},
		{"driver bad connection", errors.New("driver: bad connection")},
		{"sqlite busy", errors.New("SQLITE_BUSY: database is locked")},
		{"sqlite locked", errors.New("SQLITE_LOCKED: database table is locked")},
		{"pg 08006", &pgconn.PgError{Code: "08006", Message: "connection_failure"}},
		{"pg 40001", &pgconn.PgError{Code: "40001", Message: "serialization_failure"}},
		{"net OpError timeout", &net.OpError{Op: "read", Err: timeoutErr{}}},
		{"eof", errors.New("EOF")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFlushError(tc.err); got != "transient" {
				t.Fatalf("classifyFlushError(%v) = %q, want transient", tc.err, got)
			}
		})
	}
}

func TestClassifyFlushError_Persistent(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"unique violation", &pgconn.PgError{Code: "23505", Message: "duplicate key"}},
		{"not null violation", &pgconn.PgError{Code: "23502", Message: "not null violation"}},
		{"undefined table", &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}},
		{"generic", errors.New("something wrong happened")},
		{"invalid value", errors.New("invalid value for column")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFlushError(tc.err); got != "persistent" {
				t.Fatalf("classifyFlushError(%v) = %q, want persistent", tc.err, got)
			}
		})
	}
}

// timeoutErr implements net.Error with Timeout()=true so we can synthesise
// an OpError(timeout) without actually opening a socket.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// Mock store fails first call with transient error, succeeds on second →
// retry succeeds, counter for outcome="success" incremented, batch finishes.
func TestBatchWriterTransientRetrySucceeds(t *testing.T) {
	store := newSequencedStore(t)
	store.putAgentFailFn = func(attempt int) error {
		if attempt == 1 {
			return errors.New("dial tcp: connection refused")
		}
		return nil
	}

	sink := newRecordingSink()
	w := newTestBatchWriter(store, sink)

	w.flushAgents(context.Background(), []storage.AgentRecord{{
		ID: "agent-retry-ok", NodeName: "n", LastSeenAt: time.Now().UTC(),
	}})

	if got := sink.flushErrorCount("agents", "transient"); got != 1 {
		t.Fatalf("transient flush errors = %d, want 1", got)
	}
	if got := sink.flushErrorCount("agents", "persistent"); got != 0 {
		t.Fatalf("persistent flush errors = %d, want 0 (retry succeeded)", got)
	}
	if got := sink.retryCount("agents", "success"); got != 1 {
		t.Fatalf("retry outcome success = %d, want 1", got)
	}
	if got := sink.retryCount("agents", "exhausted"); got != 0 {
		t.Fatalf("retry outcome exhausted = %d, want 0", got)
	}
	if got := int(store.putAgentCalls.Load()); got != 2 {
		t.Fatalf("PutAgent called %d times, want 2 (fail, succeed)", got)
	}

	// Confirm the item actually landed in the store.
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "agent-retry-ok" {
		t.Fatalf("store state after retry = %+v, want one agent 'agent-retry-ok'", agents)
	}
}

// Mock store fails all 3 attempts with transient error → counter for
// outcome="exhausted" + type="transient" incremented, persistent counter
// incremented once, batch continues.
func TestBatchWriterTransientRetryExhausted(t *testing.T) {
	store := newSequencedStore(t)
	store.putAgentFailFn = func(attempt int) error {
		return errors.New("dial tcp: connection refused")
	}

	sink := newRecordingSink()
	w := newTestBatchWriter(store, sink)

	// Two items — the second must still run even though the first exhausts.
	w.flushAgents(context.Background(), []storage.AgentRecord{
		{ID: "a1", NodeName: "n", LastSeenAt: time.Now().UTC()},
		{ID: "a2", NodeName: "n", LastSeenAt: time.Now().UTC()},
	})

	// Each item: 1 first-try failure + 2 retry failures = 3 transient per item.
	// Two items => 6 transient increments total.
	if got := sink.flushErrorCount("agents", "transient"); got != 6 {
		t.Fatalf("transient flush errors = %d, want 6 (3 per item × 2 items)", got)
	}
	if got := sink.flushErrorCount("agents", "persistent"); got != 2 {
		t.Fatalf("persistent flush errors = %d, want 2 (one per exhausted item)", got)
	}
	if got := sink.retryCount("agents", "exhausted"); got != 2 {
		t.Fatalf("retry exhausted = %d, want 2", got)
	}
	if got := sink.retryCount("agents", "success"); got != 0 {
		t.Fatalf("retry success = %d, want 0", got)
	}
	// 3 attempts × 2 items = 6 PutAgent calls.
	if got := int(store.putAgentCalls.Load()); got != 6 {
		t.Fatalf("PutAgent call count = %d, want 6", got)
	}
}

// Persistent error: no retries, single counter increment for type="persistent".
func TestBatchWriterPersistentErrorSkipsRetries(t *testing.T) {
	store := newSequencedStore(t)
	store.putAgentFailFn = func(attempt int) error {
		return &pgconn.PgError{Code: "23505", Message: "unique_violation"}
	}

	sink := newRecordingSink()
	w := newTestBatchWriter(store, sink)

	w.flushAgents(context.Background(), []storage.AgentRecord{{
		ID: "dup-1", NodeName: "n", LastSeenAt: time.Now().UTC(),
	}})

	if got := sink.flushErrorCount("agents", "persistent"); got != 1 {
		t.Fatalf("persistent flush errors = %d, want 1", got)
	}
	if got := sink.flushErrorCount("agents", "transient"); got != 0 {
		t.Fatalf("transient flush errors = %d, want 0 (no retries on persistent)", got)
	}
	if got := sink.retryCount("agents", "success"); got != 0 {
		t.Fatalf("retry success = %d, want 0", got)
	}
	if got := sink.retryCount("agents", "exhausted"); got != 0 {
		t.Fatalf("retry exhausted = %d, want 0", got)
	}
	if got := int(store.putAgentCalls.Load()); got != 1 {
		t.Fatalf("PutAgent called %d times, want 1 (no retry)", got)
	}
}

// A transient error that gives up after the full schedule, then a later item
// with a persistent error, must not prevent a third item from being flushed
// successfully. Also demonstrates multi-stream independence.
func TestBatchWriterMultiStreamIndependence(t *testing.T) {
	store := newSequencedStore(t)
	// agents: persistent failure every time.
	store.putAgentFailFn = func(attempt int) error {
		return fmt.Errorf("schema mismatch: column missing")
	}
	// metrics: always transient (exhausts).
	store.appendMetricFailFn = func(attempt int) error {
		return errors.New("dial tcp: connection refused")
	}
	// dc_health: succeeds immediately.
	store.appendDCFailFn = nil

	sink := newRecordingSink()
	w := newTestBatchWriter(store, sink)

	now := time.Now().UTC()
	w.flushAgents(context.Background(), []storage.AgentRecord{{
		ID: "ag", NodeName: "n", LastSeenAt: now,
	}})
	w.flushMetrics(context.Background(), []storage.MetricSnapshotRecord{{
		ID: "m1", AgentID: "ag", CapturedAt: now,
	}})
	w.flushDCHealth(context.Background(), []storage.DCHealthPointRecord{{
		AgentID: "ag", CapturedAt: now, DC: 2,
	}})

	// agents: persistent, no retries observed.
	if got := sink.flushErrorCount("agents", "persistent"); got != 1 {
		t.Fatalf("agents persistent = %d, want 1", got)
	}
	if got := sink.flushErrorCount("agents", "transient"); got != 0 {
		t.Fatalf("agents transient = %d, want 0", got)
	}

	// metrics: exhausted -> 3 transient + 1 persistent + 1 retry-exhausted.
	if got := sink.flushErrorCount("metrics", "transient"); got != 3 {
		t.Fatalf("metrics transient = %d, want 3", got)
	}
	if got := sink.flushErrorCount("metrics", "persistent"); got != 1 {
		t.Fatalf("metrics persistent = %d, want 1", got)
	}
	if got := sink.retryCount("metrics", "exhausted"); got != 1 {
		t.Fatalf("metrics retry exhausted = %d, want 1", got)
	}

	// dc_health: no errors, no counters bumped.
	if got := sink.flushErrorCount("dc_health", "transient"); got != 0 {
		t.Fatalf("dc_health transient = %d, want 0", got)
	}
	if got := sink.flushErrorCount("dc_health", "persistent"); got != 0 {
		t.Fatalf("dc_health persistent = %d, want 0", got)
	}
	if got := int(store.appendDCCalls.Load()); got != 1 {
		t.Fatalf("AppendDCHealthPoint calls = %d, want 1 (success, no retry)", got)
	}

	// The dc_health item must have been persisted — proves the preceding
	// failing streams did not short-circuit subsequent streams.
	points, err := store.ListDCHealthPoints(context.Background(), "ag", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListDCHealthPoints: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("dc_health points persisted = %d, want 1", len(points))
	}
}

func TestStoreBatchWriterDrainsOnStop(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	w := newStoreBatchWriter(store, nil)
	w.Start()

	now := time.Now().UTC()
	w.agents.Enqueue(storage.AgentRecord{
		ID:         "agent-1",
		NodeName:   "node-a",
		LastSeenAt: now,
	})
	w.agents.Enqueue(storage.AgentRecord{
		ID:         "agent-2",
		NodeName:   "node-b",
		LastSeenAt: now,
	})

	// Stop triggers a final drain of all buffers.
	w.Stop()

	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("ListAgents() returned %d agents, want 2", len(agents))
	}
}

// P2-OBS-03: flushing a single item records a flush-duration observation
// labelled with the correct stream, regardless of success/failure. The
// recorded value must be non-negative and small for an in-memory no-op flush.
func TestBatchWriterRecordsFlushDuration(t *testing.T) {
	store := newSequencedStore(t) // no failure functions configured -> succeeds

	sink := newRecordingSink()
	w := newTestBatchWriter(store, sink)

	w.flushAgents(context.Background(), []storage.AgentRecord{{
		ID: "agent-dur-1", NodeName: "n", LastSeenAt: time.Now().UTC(),
	}})

	if got := sink.durationCount("agents"); got != 1 {
		t.Fatalf("duration observations for agents = %d, want 1", got)
	}
	if got := sink.durationCount("metrics"); got != 0 {
		t.Fatalf("duration observations for unrelated stream metrics = %d, want 0", got)
	}

	sink.mu.Lock()
	last := sink.observeCalls[len(sink.observeCalls)-1]
	sink.mu.Unlock()

	if last.stream != "agents" {
		t.Fatalf("last observation stream = %q, want %q", last.stream, "agents")
	}
	if last.seconds < 0 {
		t.Fatalf("observed duration = %v, want non-negative", last.seconds)
	}

	// A second flush — a persistent error path — must still emit a duration
	// observation so percentiles include the failure tail.
	store.putAgentFailFn = func(int) error {
		return &pgconn.PgError{Code: "23505", Message: "unique_violation"}
	}
	w.flushAgents(context.Background(), []storage.AgentRecord{{
		ID: "agent-dur-2", NodeName: "n", LastSeenAt: time.Now().UTC(),
	}})

	if got := sink.durationCount("agents"); got != 2 {
		t.Fatalf("duration observations for agents after persistent error = %d, want 2", got)
	}
}
