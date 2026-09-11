package extension

import "testing"

func TestMessageSendGuardDedup(t *testing.T) {
	g := NewMessageSendGuard()
	if !g.TryRecord(1, "m1") {
		t.Fatal("first should succeed")
	}
	if g.TryRecord(1, "m1") {
		t.Fatal("duplicate should fail")
	}
	if !g.TryRecord(1, "m2") {
		t.Fatal("different id ok")
	}
	if !g.TryRecord(2, "m1") {
		t.Fatal("different gen ok")
	}
}

func TestMessageSendGuardForgetReceipt(t *testing.T) {
	g := NewMessageSendGuard()
	if !g.TryRecord(2, "m#gen-2") {
		t.Fatal("first should succeed")
	}
	g.ForgetReceipt(EffectReceipt{
		ID:         "message-sent:m#gen-2#gen-2",
		Component:  "m#gen-2",
		Generation: 2,
	})
	if !g.TryRecord(2, "m#gen-2") {
		t.Fatal("evicted receipt did not release its dedup key")
	}
}

func TestRecordMessageSentOnce(t *testing.T) {
	owner := NewRuntimeOwner()
	if !owner.RecordMessageSentOnce(77, "once-a", "test") {
		t.Fatal("first")
	}
	if owner.RecordMessageSentOnce(77, "once-a", "test") {
		t.Fatal("second must be rejected")
	}
}
