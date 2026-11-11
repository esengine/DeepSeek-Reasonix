package process

import (
	"strings"
	"testing"
)

func TestTerminalStatesAreExactlyThoseThatEnd(t *testing.T) {
	for _, s := range States {
		live := s == Pending || s == Queued || s == Running || s == Waiting
		if s.Terminal() == live {
			t.Errorf("%s: terminal=%v", s, s.Terminal())
		}
		if !s.Valid() || strings.TrimSpace(string(s)) != string(s) {
			t.Errorf("%q is not a clean valid state", s)
		}
	}
	if State("paused").Valid() {
		t.Error("an unknown state must not be valid")
	}
}

func BenchmarkStateTerminal(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		_ = States[i%len(States)].Terminal()
	}
}

func TestIDNamesTheKindAndTheOwnersOwnID(t *testing.T) {
	if got := ID(KindJob, "j1"); got != "job:j1" {
		t.Fatalf("ID = %q, want job:j1", got)
	}
}
