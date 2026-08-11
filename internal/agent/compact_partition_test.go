package agent

import (
	"fmt"
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

func TestPartitionKeepsSmallUserTurnsVerbatim(t *testing.T) {
	// A constraint stated mid-session cannot be re-derived once a digest drops
	// it, so small user turns never reach the summarizer's judgement.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small turn 0"},
		{Role: provider.RoleUser, Content: "small turn 1"},
		{Role: provider.RoleUser, Content: "small turn 2"},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 3 || len(fold) != 1 {
		t.Fatalf("kept=%d fold=%d, want 3/1", len(kept), len(fold))
	}
	if got := renderTranscript(fold); strings.Contains(got, "small turn") {
		t.Fatalf("no user turn should reach the summarizer: %s", got)
	}
}

func TestPartitionFoldsUserTurnsPastBudget(t *testing.T) {
	// Unbounded hoisting is what padded an earlier revision's candidates past
	// the acceptance ceiling, so oversize and over-budget turns still fold.
	a := &Agent{}
	oversize := provider.Message{
		Role:    provider.RoleUser,
		Content: strings.Repeat("y", maxKeptUserTurnTokens*8),
	}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small and kept"},
		oversize,
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 1 || len(fold) != 1 {
		t.Fatalf("kept=%d fold=%d, want the oversize turn folded", len(kept), len(fold))
	}
	if !strings.Contains(renderTranscript(kept), "small and kept") {
		t.Fatal("the small turn should survive alongside a folded oversize one")
	}
}

func TestPartitionUserTurnBudgetIsBoundedInTotal(t *testing.T) {
	a := &Agent{}
	// Each turn is a quarter of the per-turn ceiling, so the total budget —
	// not the per-turn one — is what must stop retention.
	each := strings.Repeat("z", maxKeptUserTurnTokens)
	region := make([]provider.Message, 0, 32)
	for i := range 32 {
		// Distinct content: partitionCoversRegion keys its coverage check on it.
		region = append(region, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("%02d%s", i, each)})
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(fold) == 0 {
		t.Fatal("total budget never engaged: every turn was kept")
	}
	budget := keptUserTurnsBudgetTokens
	spent := 0
	for _, m := range kept {
		spent += fixedTokenEstimate(m)
	}
	if spent > budget {
		t.Fatalf("kept %d tokens of user turns, over the %d budget", spent, budget)
	}
}

func TestPartitionKeepsUserTurnsAcrossPriorDigest(t *testing.T) {
	// The keep policy is scoped to messages after the latest digest so it cannot
	// grow forever; user turns are bounded by budget instead, so a turn from
	// before the digest must still survive the next fold.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "constraint from before the digest"},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nprior digest\n" + summaryTagClose},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 1 || !strings.Contains(renderTranscript(kept), "constraint from before") {
		t.Fatalf("pre-digest user turn must survive; kept=%v", renderTranscript(kept))
	}
	if !strings.Contains(renderTranscript(fold), "prior digest") {
		t.Fatal("the digest itself must still fold into the next one")
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
		{Role: provider.RoleAssistant, Content: "unrelated prose"},
		{Role: provider.RoleAssistant, Content: "call", ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "bash", Content: "error: boom"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 2 || len(fold) != 1 {
		t.Fatalf("kept=%d fold=%d, want the error tool-call group kept and the prose folded", len(kept), len(fold))
	}
	if got := renderTranscript(kept); !strings.Contains(got, "error: boom") || !strings.Contains(got, "call") {
		t.Fatalf("the failing call and its result must be kept together: %s", got)
	}
}
