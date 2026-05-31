package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// alwaysFailAuditStore embeds the storage.Store interface (so it satisfies the
// type) but only implements AppendAuditEvent, which always returns a
// non-retriable ("persistent") error. flushAuditEvents calls no other Store
// method, so the embedded nil interface is never dereferenced.
type alwaysFailAuditStore struct {
	storage.Store
	err error
}

func (s alwaysFailAuditStore) AppendAuditEvent(context.Context, storage.AuditEventRecord) error {
	return s.err
}

// newDeadLetterTestWriter builds a storeBatchWriter wired to a store whose
// audit writes always fail persistently, with the dead-letter dir pointed at a
// throwaway temp directory.
func newDeadLetterTestWriter(dir string) *storeBatchWriter {
	w := &storeBatchWriter{
		store:         alwaysFailAuditStore{err: errors.New("simulated persistent audit failure")},
		metrics:       noopMetricsSink{},
		done:          make(chan struct{}),
		sleep:         func(time.Duration) {},
		now:           func() time.Time { return time.Unix(0, 0).UTC() },
		deadLetterDir: dir,
	}
	w.writeDeadLetter = w.writeAuditDeadLetter
	return w
}

// TestAuditFlushPermanentFailureSpoolsToDeadLetter verifies the A4 contract:
// when an audit batch exhausts its in-memory flush retries (here: an immediate
// persistent error), the record is written to the on-disk dead-letter JSONL
// file rather than dropped silently.
func TestAuditFlushPermanentFailureSpoolsToDeadLetter(t *testing.T) {
	dir := t.TempDir()
	w := newDeadLetterTestWriter(dir)

	want := storage.AuditEventRecord{ID: "audit-123", Action: "user.login"}
	w.flushAuditEvents(context.Background(), []storage.AuditEventRecord{want})

	path := filepath.Join(dir, auditDeadLetterFileName)
	f, err := os.Open(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("expected dead-letter file at %s, got error: %v", path, err)
	}
	defer f.Close()

	var lines []deadLetteredAuditEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var dl deadLetteredAuditEvent
		if err := json.Unmarshal(sc.Bytes(), &dl); err != nil {
			t.Fatalf("dead-letter line is not valid JSON: %v", err)
		}
		lines = append(lines, dl)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning dead-letter file: %v", err)
	}

	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 dead-lettered audit event, got %d", len(lines))
	}
	if lines[0].Event.ID != want.ID || lines[0].Event.Action != want.Action {
		t.Fatalf("dead-lettered event = %+v; want ID=%q Action=%q",
			lines[0].Event, want.ID, want.Action)
	}
}

// TestAuditDeadLetterWriteFailureDoesNotPanic verifies that when the
// dead-letter spool itself fails, flushAuditEvents logs and continues rather
// than panicking or losing the rest of the batch.
func TestAuditDeadLetterWriteFailureDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	w := newDeadLetterTestWriter(dir)

	spoolCalls := 0
	w.writeDeadLetter = func(storage.AuditEventRecord) error {
		spoolCalls++
		return errors.New("disk full")
	}

	// Two records: both fail to persist and both fail to spool. The loop must
	// attempt to spool each one without aborting.
	items := []storage.AuditEventRecord{{ID: "a"}, {ID: "b"}}
	w.flushAuditEvents(context.Background(), items)

	if spoolCalls != len(items) {
		t.Fatalf("expected %d dead-letter spool attempts, got %d", len(items), spoolCalls)
	}
}
