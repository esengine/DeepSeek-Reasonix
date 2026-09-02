package control

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/skill"
)

// The listing is delivered once, on a user turn. A fold can summarise that turn
// away, and standing state that is only ever said once does not survive that —
// so a completed compaction has to owe it again.
func TestSkillCatalogIsOwedAgainAfterAFold(t *testing.T) {
	c := New(Options{Skills: []skill.Skill{{Name: "alpha", Description: "does the alpha thing"}}})

	first := c.Compose("hello")
	if !strings.Contains(first, "<available-skills>") || !strings.Contains(first, "alpha") {
		t.Fatalf("the first turn did not carry the listing: %q", first)
	}
	if second := c.Compose("again"); strings.Contains(second, "<available-skills>") {
		t.Fatalf("the listing was delivered twice without anything changing: %q", second)
	}

	c.sink.Emit(event.Event{Kind: event.CompactionDone})

	if after := c.Compose("after the fold"); !strings.Contains(after, "<available-skills>") || !strings.Contains(after, "alpha") {
		t.Fatalf("the fold left the session without a listing: %q", after)
	}
}
