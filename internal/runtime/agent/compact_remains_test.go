package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func TestRemainsAfterFoldOnlyWhenTheRestIsALargeShareOfTheWindow(t *testing.T) {
	sess := sessionstore.NewSession("system prompt")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "go", CreatedAt: 1})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 40_000)})
	big := New(nil, nil, sess, Options{ContextWindow: 20_000}, nil).window().remainsAfterFold()
	if big == nil || len(big.Parts) == 0 || big.Parts[0].Name != "reply" || big.Total <= 0 {
		t.Fatalf("a prompt over a quarter of the window was not reported: %+v", big)
	}
	if small := New(nil, nil, sess, Options{ContextWindow: 10_000_000}, nil).window().remainsAfterFold(); small != nil {
		t.Fatalf("a small remainder was reported: %+v", small)
	}
	unknown := New(nil, nil, sess, Options{}, nil).window().remainsAfterFold()
	if unknown != nil {
		t.Fatalf("with no declared window and a remainder under the floor nothing should be said: %+v", unknown)
	}
}

// What a fold leaves is the number a person asks about; the event carries what
// it is made of so the card does not have to leave the question open.
func TestCompactionDoneCarriesWhatIsLeftWhenItIsStillLarge(t *testing.T) {
	var done []event.Compaction
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionDone {
			done = append(done, e.Compaction)
		}
	})
	run := func(window int) event.Compaction {
		done = nil
		a := New(&fakeProvider{reply: "## Summary\n- work"}, nil, foldableSessionOverForce(40),
			Options{ContextWindow: window, CompactRatio: 0.5, ArchiveDir: testenv.TempDir(t)}, sink)
		if _, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil {
			t.Fatalf("compactToProjection: %v", err)
		}
		if len(done) != 1 {
			t.Fatalf("CompactionDone events = %d, want 1", len(done))
		}
		return done[0]
	}
	tight := run(150)
	if tight.Remains == nil || tight.Remains.Total <= 0 || len(tight.Remains.Parts) == 0 {
		t.Fatalf("a fold that left most of a small window said nothing about what is left: %+v", tight.Remains)
	}
	if roomy := run(50_000_000); roomy.Remains != nil {
		t.Fatalf("a fold that left a sliver reported a composition: %+v", roomy.Remains)
	}
}
