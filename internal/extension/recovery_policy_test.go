package extension

import "testing"

func TestDecideResumeIrreversibleNeverClean(t *testing.T) {
	s := NewReceiptStore()
	s.Record(EffectReceipt{ID: "p1", Class: Irreversible, Generation: 7, CompensationStatus: "rolled_back"})
	d := DecideResume(s, 7)
	if !d.AllowResume {
		t.Fatal("must allow resume with awareness")
	}
	if d.CleanRollback {
		t.Fatal("irreversible must not claim clean rollback")
	}
	if !d.HasIrreversible {
		t.Fatal("expected irreversible flag")
	}
	// Store must not keep a fake rolled_back status.
	r, ok := s.Get("p1")
	if !ok || r.CompensationStatus == "rolled_back" {
		t.Fatalf("receipt = %+v", r)
	}
}

func TestDecideResumeCleanWhenEmpty(t *testing.T) {
	s := NewReceiptStore()
	d := DecideResume(s, 1)
	if !d.AllowResume || !d.CleanRollback {
		t.Fatalf("decision = %+v", d)
	}
}

func TestDecideResumeCompensatableFailed(t *testing.T) {
	s := NewReceiptStore()
	s.Record(EffectReceipt{ID: "c1", Class: Compensatable, Generation: 2, CompensationStatus: "failed"})
	d := DecideResume(s, 2)
	if d.CleanRollback {
		t.Fatal("failed compensation must not be clean")
	}
	if len(d.Blocking) == 0 {
		t.Fatal("expected blocking id")
	}
}
