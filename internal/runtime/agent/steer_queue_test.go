package agent

import "testing"

// The queue's own lock is what makes a cancel an answer rather than a guess: an
// entry is either still here to be taken back or already read, and there is no
// third state a caller has to interpret.
func TestSteerQueueGivesBackOnlyWhatItStillHolds(t *testing.T) {
	var q steerInbox
	q.open()
	if !q.admit(steerEntry{itemID: "a"}) || !q.admit(steerEntry{itemID: "b"}) {
		t.Fatal("intake refused an open queue")
	}
	if q.remove("") || q.remove("never-queued") {
		t.Fatal("removed something that was never queued")
	}
	if !q.remove("a") {
		t.Fatal("a queued entry could not be taken back")
	}
	if got := q.pending(); got != 1 {
		t.Fatalf("pending = %d, want the untouched entry to stay", got)
	}
	e, ok := q.take()
	if !ok || e.itemID != "b" {
		t.Fatalf("take = %+v ok=%v, want the entry that was left", e, ok)
	}
	if q.remove("b") {
		t.Fatal("an entry the loop had already read was reported as taken back")
	}
}

// Guidance the host queued for itself carries no item id, and a cancel that
// matched on anything else would reach it.
func TestSteerQueueLeavesHostNoticesAlone(t *testing.T) {
	var q steerInbox
	q.open()
	q.admit(steerEntry{host: true, load: func() (string, error) { return "context is tight", nil }})
	if q.remove("") {
		t.Fatal("a host notice was removed by an empty id")
	}
	if got := q.pending(); got != 1 {
		t.Fatalf("pending = %d, want the host notice kept", got)
	}
}
