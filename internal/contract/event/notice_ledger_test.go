package event

import "testing"

func TestNoticeLedgerWindows(t *testing.T) {
	var got []Event
	sink := FuncSink(func(e Event) { got = append(got, e) })
	l := NewNoticeLedger()
	a := Event{Kind: Notice, Code: "a", Text: "x"}

	w := l.Begin(sink)
	w.Emit(a)
	w.Emit(Event{Kind: Text, Text: "delta"})
	w.Close(true)
	w.Emit(a)
	if len(got) != 3 {
		t.Fatalf("first build and post-close runtime must emit all: %d", len(got))
	}

	got = nil
	w = l.Begin(sink)
	w.Emit(a)
	w.Emit(Event{Kind: Notice, Code: "a", Text: "x", Detail: "changed"})
	w.Close(false)
	if len(got) != 1 || got[0].Detail != "changed" {
		t.Fatalf("repeat dropped, changed kept: %+v", got)
	}

	got = nil
	w = l.Begin(sink)
	w.Emit(Event{Kind: Notice, Code: "a", Text: "x", Detail: "changed"})
	w.Close(true)
	if len(got) != 0 {
		t.Fatalf("a failed build's notices count as shown: %+v", got)
	}
}
