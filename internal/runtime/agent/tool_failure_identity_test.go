package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// A tool result the host refused carries the host's own identity for it. Only
// shell calls recorded one before: everything else left the fact in the words,
// and a reader had to match "error:" against prose the tool wrote. Measured
// across real sessions, 433 refused results after the shell path was fixed had
// nothing else to go on.
func TestRefusedResultCarriesItsIdentity(t *testing.T) {
	refused := provider.Message{
		Role:        provider.RoleTool,
		Content:     "the model was told this in its own words",
		ToolFailure: &provider.ToolFailure{RefusalCode: "goal.no_active_turn"},
	}
	if !isErrorMessage(refused) {
		t.Fatal("a refusal the host recorded did not read as one")
	}
	plain := provider.Message{Role: provider.RoleTool, Content: "the model was told this in its own words"}
	if isErrorMessage(plain) {
		t.Fatal("the same words without the record read as a failure, so the identity decided nothing")
	}
}

// A tool whose own output opens with the word is not a refusal. The prefix rule
// still answers for transcripts written before the host recorded anything, so
// this states which one is the rule and which the fallback.
func TestTheWordsRemainTheFallback(t *testing.T) {
	old := provider.Message{Role: provider.RoleTool, Content: "error: written before the host recorded it"}
	if !isErrorMessage(old) {
		t.Fatal("an older transcript lost its only failure signal")
	}
}

// Local metadata never reaches a provider request, and survives the projection
// the next compaction reads — the split ToolExecution already makes.
func TestFailureIdentityStopsAtTheProviderBoundary(t *testing.T) {
	msgs := []provider.Message{{
		Role:        provider.RoleTool,
		Content:     "refused",
		ToolFailure: &provider.ToolFailure{RefusalCode: "sandbox.write_outside_root", Blocked: true},
	}}
	if sent := provider.ModelMessages(msgs); sent[0].ToolFailure != nil {
		t.Fatalf("the record reached a provider request: %+v", sent[0].ToolFailure)
	}
	kept := provider.ProjectionMessages(msgs)
	if kept[0].ToolFailure == nil || kept[0].ToolFailure.RefusalCode != "sandbox.write_outside_root" {
		t.Fatalf("the stored projection dropped it, so the next fold cannot classify: %+v", kept[0].ToolFailure)
	}
	if strings.Contains(kept[0].Content, "sandbox") {
		t.Fatal("fixture no longer separates the identity from the words")
	}
}
