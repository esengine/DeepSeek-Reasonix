package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeOwnerFallbackIsObservable(t *testing.T) {
	before := RuntimeOwnerFallbackCount()
	if got := RuntimeOwnerFromContext(context.Background()); got != DefaultRuntimeOwner {
		t.Fatal("unbound context must use the compatibility owner")
	}
	if after := RuntimeOwnerFallbackCount(); after <= before {
		t.Fatalf("fallback count = %d, want greater than %d", after, before)
	}
}

func TestRuntimeOwnerRepeatedFileWritesKeepDistinctPriors(t *testing.T) {
	owner := NewRuntimeOwner()
	owner.Gate.Publish(7)
	path := filepath.Join(t.TempDir(), "same.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := owner.RecordFileWrite(path, true, []byte("old"))
	if err := os.WriteFile(path, []byte("middle"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := owner.RecordFileWrite(path, true, []byte("middle"))
	if err := os.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Fatal("repeated writes must receive distinct receipt IDs")
	}
	if receipts := owner.Receipts.ForGeneration(7); len(receipts) != 2 {
		t.Fatalf("receipts = %+v", receipts)
	}
	if err := owner.ApplyFileWriteCompensation(second); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "middle" {
		t.Fatalf("second prior = %q", got)
	}
	if err := owner.ApplyFileWriteCompensation(first); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("first prior = %q", got)
	}
	for _, id := range []string{first, second} {
		r, ok := owner.Receipts.Get(id)
		if !ok || r.CompensationStatus != "applied" {
			t.Fatalf("receipt %s = %+v, ok=%v", id, r, ok)
		}
	}
}

func TestRuntimeOwnerReceiptEvictionReleasesFilePrior(t *testing.T) {
	owner := NewRuntimeOwner()
	owner.Gate.Publish(11)
	path := filepath.Join(t.TempDir(), "bounded.txt")
	first := owner.RecordFileWrite(path, true, []byte("old"))
	for i := 1; i <= defaultReceiptPerGenerationLimit; i++ {
		owner.RecordFileWrite(path, true, []byte("old"))
	}

	if _, ok := owner.Receipts.Get(first); ok {
		t.Fatal("oldest receipt should be evicted at the per-generation limit")
	}
	if err := owner.FilePriors.Compensate(first); err == nil {
		t.Fatal("evicted receipt must release its retained file prior")
	}
	if rec := owner.AssessRecoverability(11); rec.Clean {
		t.Fatalf("truncated receipt history must not claim clean recovery: %+v", rec)
	}
}

func TestRuntimeOwnerFilePriorBudgetMarksRecoveryTruncated(t *testing.T) {
	owner := NewRuntimeOwner()
	owner.FilePriors = newFilePriorStore(1, 1)
	owner.Gate.Publish(12)
	id := owner.RecordFileWrite(filepath.Join(t.TempDir(), "oversized"), true, []byte("12"))
	receipt, ok := owner.Receipts.Get(id)
	if !ok || receipt.CompensationStatus != "prior_truncated" {
		t.Fatalf("receipt = %+v ok=%v, want prior_truncated", receipt, ok)
	}
	if rec := owner.AssessRecoverability(12); rec.Clean {
		t.Fatalf("missing prior bytes must not claim clean recovery: %+v", rec)
	}
}

func TestRuntimeOwnerReceiptEvictionReleasesMessageDedup(t *testing.T) {
	owner := NewRuntimeOwner()
	const gen = uint64(13)
	if !owner.RecordMessageSentOnce(gen, "oldest", "test") {
		t.Fatal("first message receipt was rejected")
	}
	for i := range defaultReceiptPerGenerationLimit {
		owner.Receipts.Record(EffectReceipt{
			ID:         "later-" + itoaU64(uint64(i)),
			Generation: gen,
			Class:      Irreversible,
		})
	}
	if _, ok := owner.Receipts.Get("message-sent:oldest"); ok {
		t.Fatal("oldest message receipt should have been evicted")
	}
	if !owner.RecordMessageSentOnce(gen, "oldest", "test") {
		t.Fatal("evicted message receipt kept an unbounded dedup key")
	}
}
