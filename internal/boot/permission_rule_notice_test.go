package boot

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

type captureSink struct{ events []event.Event }

func (c *captureSink) Emit(e event.Event) { c.events = append(c.events, e) }

// TestUnmatchableRuleNoticeReachesTheSink is the reason this warning exists:
// config load warnings are only read by the desktop, so a rule typed into
// config.toml had to reach the transport-agnostic sink to be seen by a CLI
// user at all.
func TestUnmatchableRuleNoticeReachesTheSink(t *testing.T) {
	sink := &captureSink{}
	emitUnmatchableRuleNotice(sink, nil, nil, []string{"git push --force"})
	if len(sink.events) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.events))
	}
	got := sink.events[0]
	if got.Kind != event.Notice || got.Level != event.LevelWarn {
		t.Fatalf("event = %v/%v, want Notice/LevelWarn", got.Kind, got.Level)
	}
	if !strings.Contains(got.Detail, "permissions.deny") {
		t.Errorf("detail does not name the setting: %q", got.Detail)
	}
	if !strings.Contains(got.Detail, `Bash(git push --force:*)`) {
		t.Errorf("detail does not carry the rewrite: %q", got.Detail)
	}
}

// TestUnmatchableRuleNoticeSilentWhenRulesWork keeps startup quiet for the
// configs that are already correct.
func TestUnmatchableRuleNoticeSilentWhenRulesWork(t *testing.T) {
	sink := &captureSink{}
	emitUnmatchableRuleNotice(sink, []string{"Bash(git status:*)"}, nil, []string{"edit_file"})
	if len(sink.events) != 0 {
		t.Fatalf("a correct config emitted %d events: %+v", len(sink.events), sink.events)
	}
}
