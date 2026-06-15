package memory

import (
	"context"
	"testing"
)

func TestTraceRingNewestFirst(t *testing.T) {
	r := NewTraceRing(2)
	r.Record(&RetrievalTrace{RequestID: "a"})
	r.Record(&RetrievalTrace{RequestID: "b"})
	r.Record(&RetrievalTrace{RequestID: "c"}) // evicts "a"

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("len = %d, want 2", len(snap))
	}
	if snap[0].RequestID != "c" || snap[1].RequestID != "b" {
		t.Errorf("order = %q,%q; want c,b", snap[0].RequestID, snap[1].RequestID)
	}
}

func TestTraceRingEmpty(t *testing.T) {
	r := NewTraceRing(4)
	if got := r.Snapshot(); len(got) != 0 {
		t.Errorf("empty ring snapshot len = %d, want 0", len(got))
	}
}

func TestTraceHolderViaContext(t *testing.T) {
	ctx, holder := WithTrace(context.Background())
	if traceHolderFrom(ctx) == nil {
		t.Fatal("holder not planted in context")
	}
	tr := &RetrievalTrace{RequestID: "x"}
	traceHolderFrom(ctx).Set(tr)
	if holder.Get() != tr {
		t.Error("holder did not capture the trace")
	}
}

func TestTraceHolderAbsent(t *testing.T) {
	if traceHolderFrom(context.Background()) != nil {
		t.Error("expected nil holder on bare context")
	}
}
