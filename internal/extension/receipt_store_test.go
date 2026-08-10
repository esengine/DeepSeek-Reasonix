package extension

import (
	"context"
	"testing"
)

func TestReceiptStoreIrreversibleNeverRolledBack(t *testing.T) {
	s := NewReceiptStore()
	s.Record(EffectReceipt{ID: "msg-1", Class: Irreversible, Generation: 3, CompensationStatus: "rolled_back"})
	r, ok := s.Get("msg-1")
	if !ok || r.CompensationStatus != "not_applicable" {
		t.Fatalf("receipt = %+v", r)
	}
	rec := s.AssessRecoverability(3)
	if rec.Clean || !rec.HasIrreversible {
		t.Fatalf("recoverability = %+v", rec)
	}
}

func TestReceiptStoreIngestScope(t *testing.T) {
	scope := NewEffectScope(7)
	_ = scope.Track(Effect{
		ID: "file-write", Owner: "test", Class: Compensatable,
		Dispose:    func(context.Context) error { return nil },
		Compensate: func(context.Context) error { return nil },
	})
	_ = scope.Dispose(context.Background())
	s := NewReceiptStore()
	s.IngestScope(scope)
	if len(s.ForGeneration(7)) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(s.ForGeneration(7)))
	}
}

func TestReceiptStoreKeepsSameIDAcrossGenerations(t *testing.T) {
	s := NewReceiptStore()
	s.Record(EffectReceipt{ID: "shared", Owner: "old", Generation: 1, Class: Irreversible})
	s.Record(EffectReceipt{ID: "shared", Owner: "new", Generation: 2, Class: Irreversible})

	oldReceipts := s.ForGeneration(1)
	newReceipts := s.ForGeneration(2)
	if len(oldReceipts) != 1 || oldReceipts[0].Owner != "old" || oldReceipts[0].ID != "shared" {
		t.Fatalf("old receipts = %+v", oldReceipts)
	}
	if len(newReceipts) != 1 || newReceipts[0].Owner != "new" || newReceipts[0].ID == "shared" {
		t.Fatalf("new receipts = %+v", newReceipts)
	}
	if old, ok := s.Get("shared"); !ok || old.Owner != "old" {
		t.Fatalf("original receipt was overwritten: %+v ok=%v", old, ok)
	}
}

func TestReceiptStoreBoundsGenerationAndReceiptRetention(t *testing.T) {
	s := newReceiptStore(2, 2, nil)
	for gen := uint64(1); gen <= 3; gen++ {
		for i := range 2 {
			s.Record(EffectReceipt{ID: itoaU64(gen) + "-" + itoaU64(uint64(i)), Generation: gen, Class: Irreversible})
		}
	}
	if got := len(s.ForGeneration(1)); got != 0 {
		t.Fatalf("evicted generation receipts = %d, want 0", got)
	}
	if rec := s.AssessRecoverability(1); rec.Clean || len(rec.Blocking) == 0 {
		t.Fatalf("evicted generation must not claim clean recovery: %+v", rec)
	}

	for i := range 3 {
		s.Record(EffectReceipt{ID: "current-" + itoaU64(uint64(i)), Generation: 4, Class: Irreversible})
	}
	if got := len(s.ForGeneration(4)); got != 2 {
		t.Fatalf("retained receipts = %d, want 2", got)
	}
	if rec := s.AssessRecoverability(4); rec.Clean || len(rec.Blocking) == 0 {
		t.Fatalf("truncated generation must not claim clean recovery: %+v", rec)
	}
}

func TestReceiptStoreTruncatedPriorBlocksCleanRecovery(t *testing.T) {
	s := NewReceiptStore()
	s.Record(EffectReceipt{
		ID:                 "large-prior",
		Generation:         5,
		Class:              Compensatable,
		CompensationStatus: "prior_truncated",
	})
	if rec := s.AssessRecoverability(5); rec.Clean || len(rec.Blocking) != 1 {
		t.Fatalf("truncated prior recoverability = %+v", rec)
	}
}
