package runtime

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// snapshotWith returns a runtime sample whose CurrentConnections counter
// encodes the supplied id, so an oldest-first drain can be asserted by
// reading back the ordering of the field.
func sampleWithID(id int32) RuntimeSample {
	return RuntimeSample{
		ObservedAt: time.Unix(int64(id), 0).UTC(),
		Snapshot: &gatewayrpc.Snapshot{
			Runtime: &gatewayrpc.RuntimeSnapshot{
				CurrentConnections: id,
			},
		},
	}
}

func TestRingBufferPushAndDrainOrderBelowCapacity(t *testing.T) {
	rb := NewRuntimeRingBuffer(4)
	for i := int32(1); i <= 3; i++ {
		rb.Push(sampleWithID(i))
	}
	got := rb.snapshotOrdered()
	if len(got) != 3 {
		t.Fatalf("snapshotOrdered length = %d, want 3", len(got))
	}
	for i, s := range got {
		want := int32(i + 1)
		if s.Snapshot.GetRuntime().GetCurrentConnections() != want {
			t.Fatalf("sample[%d] id = %d, want %d", i,
				s.Snapshot.GetRuntime().GetCurrentConnections(), want)
		}
	}
	if rb.DroppedCount() != 0 {
		t.Fatalf("DroppedCount = %d, want 0", rb.DroppedCount())
	}
}

func TestRingBufferOverflowKeepsNewestSamples(t *testing.T) {
	rb := NewRuntimeRingBuffer(3)
	// Push 7 samples into a cap-3 buffer; expect the last three
	// (5, 6, 7) to remain in oldest-first order.
	for i := int32(1); i <= 7; i++ {
		rb.Push(sampleWithID(i))
	}
	got := rb.snapshotOrdered()
	if len(got) != 3 {
		t.Fatalf("snapshotOrdered length = %d, want 3", len(got))
	}
	wantIDs := []int32{5, 6, 7}
	for i, s := range got {
		if s.Snapshot.GetRuntime().GetCurrentConnections() != wantIDs[i] {
			t.Fatalf("sample[%d] id = %d, want %d", i,
				s.Snapshot.GetRuntime().GetCurrentConnections(), wantIDs[i])
		}
	}
	if rb.DroppedCount() != 4 {
		t.Fatalf("DroppedCount = %d, want 4", rb.DroppedCount())
	}
}

func TestRingBufferDrainResetsState(t *testing.T) {
	rb := NewRuntimeRingBuffer(2)
	rb.Push(sampleWithID(1))
	rb.Push(sampleWithID(2))
	if got := rb.snapshotOrdered(); len(got) != 2 {
		t.Fatalf("first drain length = %d, want 2", len(got))
	}
	// After drain the buffer must report empty.
	if got := rb.snapshotOrdered(); got != nil {
		t.Fatalf("second drain = %v, want nil", got)
	}
	// Pushing again must not see the previous samples.
	rb.Push(sampleWithID(99))
	got := rb.snapshotOrdered()
	if len(got) != 1 || got[0].Snapshot.GetRuntime().GetCurrentConnections() != 99 {
		t.Fatalf("post-drain push readout = %+v, want single id=99", got)
	}
}
