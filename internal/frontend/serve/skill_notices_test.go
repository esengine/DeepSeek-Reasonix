package serve

import (
	"testing"

	"reasonix/internal/contract/event"
)

type collectSink struct{ got []event.Event }

func (c *collectSink) Emit(e event.Event) { c.got = append(c.got, e) }

func warn(text string) event.Event {
	return event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text}
}

// A skill problem is a fact about a file, not about a window: every window the
// host opens rediscovers it, and the person has already been told.
func TestASkillWarningIsShownOncePerHostNotOncePerWindow(t *testing.T) {
	var said skillNotices
	first, second := &collectSink{}, &collectSink{}
	a, b := said.once(first), said.once(second)

	a.Emit(warn("warning: skill \"x\" at /s/x.md has no description"))
	b.Emit(warn("warning: skill \"x\" at /s/x.md has no description"))
	b.Emit(warn("warning: skill \"y\" at /s/y.md has no description"))
	a.Emit(warn("warning: skill \"y\" at /s/y.md has no description"))

	if len(first.got) != 1 || len(second.got) != 1 || second.got[0].Text != "warning: skill \"y\" at /s/y.md has no description" {
		t.Fatalf("first window %+v, second window %+v; each distinct warning should reach exactly one window, the first to rediscover it", first.got, second.got)
	}
}

// What the file says now is shown even when an earlier text was already said.
func TestAChangedSkillWarningIsShownAgain(t *testing.T) {
	var said skillNotices
	sink := &collectSink{}
	w := said.once(sink)
	w.Emit(warn("warning: skill \"x\" has no description"))
	w.Emit(warn("warning: skill \"x\" has no description: frontmatter does not parse"))
	if len(sink.got) != 2 {
		t.Fatalf("got %+v, want both texts", sink.got)
	}
}

// A server no hub claimed has nobody to share with: its own sink, unchanged.
func TestAServerWithoutAHubEmitsSkillWarningsIntoItsOwnSink(t *testing.T) {
	bc := NewBroadcaster()
	s := &Server{bc: bc}
	if got := s.diagnosticSink(); got != event.Sink(bc) {
		t.Fatalf("diagnosticSink = %T, want the broadcaster", got)
	}
	var said skillNotices
	s.skillNotices = &said
	if got := s.diagnosticSink(); got == event.Sink(bc) {
		t.Fatal("a server a hub claimed still emits skill warnings unfiltered")
	}
}
