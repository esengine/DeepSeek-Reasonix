package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// partitionCoversRegion is the invariant that makes a lost user turn impossible:
// every provider-visible message in the region lands in exactly one group.
func partitionCoversRegion(t *testing.T, a *Agent, region []provider.Message) (kept, fold []provider.Message) {
	t.Helper()
	early, carried, kept, fold := a.partitionFoldForProjection(region)
	if len(early) != 0 || len(carried) != 0 {
		t.Fatalf("early=%d carried=%d, want both empty under content-driven summary", len(early), len(carried))
	}
	seen := map[string]int{}
	for _, group := range [][]provider.Message{kept, fold} {
		for _, m := range group {
			seen[m.Content]++
		}
	}
	for _, m := range region {
		if m.LocalOnly {
			if seen[m.Content] != 0 {
				t.Errorf("display-only message %q reached the projection", m.Content)
			}
			continue
		}
		switch seen[m.Content] {
		case 1:
		case 0:
			t.Errorf("message %q is in no group — it would vanish from the projection", m.Content)
		default:
			t.Errorf("message %q is in %d groups — it would be duplicated", m.Content, seen[m.Content])
		}
	}
	return kept, fold
}

func TestPartitionFoldsAllUserTurnsByDefault(t *testing.T) {
	// Content-driven summary no longer hoists early user turns to pad the
	// checkpoint; only keep-policy content and the recent tail stay verbatim.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small turn 0"},
		{Role: provider.RoleUser, Content: "small turn 1"},
		{Role: provider.RoleUser, Content: "small turn 2"},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 0 || len(fold) != 4 {
		t.Fatalf("kept=%d fold=%d, want 0/4", len(kept), len(fold))
	}
}

func TestPartitionFoldsPriorDigests(t *testing.T) {
	// Any digest in the region is merged into the next one, so the model
	// never sees a chain of digests.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nolder digest\n" + summaryTagClose},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nnewer digest\n" + summaryTagClose},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 0 || len(fold) != 3 {
		t.Fatalf("kept=%d fold=%d, want every digest folded", len(kept), len(fold))
	}
}

func TestPartitionKeepPolicyOutranksFold(t *testing.T) {
	a := &Agent{keepPolicy: KeepErrors}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small"},
		{Role: provider.RoleAssistant, Content: "call", ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "bash", Content: "error: boom"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 2 || len(fold) != 1 {
		t.Fatalf("kept=%d fold=%d, want error tool-call group kept and user folded", len(kept), len(fold))
	}
	if got := renderTranscript(fold); !strings.Contains(got, "small") {
		t.Fatalf("user turn should fold: %s", got)
	}
}
