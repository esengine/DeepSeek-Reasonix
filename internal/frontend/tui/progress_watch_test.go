package tui

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

func TestProgressWatchSaysAStallOncePerEpisode(t *testing.T) {
	var tr Transcript
	stalled := func(n int) eventwire.Event {
		return eventwire.Event{Kind: "progress_watch", ProgressWatch: &eventwire.ProgressWatch{Stalled: true, Cause: "rounds", IdleRounds: n, RoundLimit: 20}}
	}
	tr.Apply(stalled(20))
	tr.Apply(stalled(21))
	if n := len(tr.Items); n != 1 || tr.Items[0].Kind != ItemNotice {
		t.Fatalf("items = %+v, want one notice for one stall", tr.Items)
	}
	tr.Apply(eventwire.Event{Kind: "progress_watch", ProgressWatch: &eventwire.ProgressWatch{}})
	tr.Apply(stalled(20))
	// Said again, folded into the row it repeats rather than stacked under it.
	if len(tr.Items) != 1 || tr.Items[0].Count != 2 {
		t.Fatalf("a new stall after it cleared was not said: %+v", tr.Items)
	}
}

// A perseveration stall reads as perseveration, not as the round or token cause.
func TestProgressWatchSaysPerseveration(t *testing.T) {
	var tr Transcript
	tr.Apply(eventwire.Event{Kind: "progress_watch", ProgressWatch: &eventwire.ProgressWatch{
		Stalled: true, Cause: "perseveration",
	}})
	if len(tr.Items) != 1 || tr.Items[0].Kind != ItemNotice {
		t.Fatalf("items = %+v, want one perseveration notice", tr.Items)
	}
	if text := tr.Items[0].Text; !strings.Contains(text, "repeating the same text") {
		t.Fatalf("perseveration notice = %q", text)
	}
}
