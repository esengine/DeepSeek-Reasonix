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
	kept, fold, retention := a.partitionFoldForProjection(region)
	userTurns := 0
	for _, m := range region {
		if m.Role == provider.RoleUser && !m.LocalOnly && !isCompactionSummary(m) {
			userTurns++
		}
	}
	if got := retention.Kept + retention.Dropped; got != userTurns {
		t.Errorf("retention accounts for %d user turns, region has %d", got, userTurns)
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

func TestPartitionFoldsSmallUserTurns(t *testing.T) {
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
	if got := renderTranscript(fold); !strings.Contains(got, "small turn") {
		t.Fatalf("old user turns must reach the summarizer: %s", got)
	}
}

func TestPartitionFoldsLargeUserTurns(t *testing.T) {
	a := &Agent{}
	oversize := provider.Message{
		Role:    provider.RoleUser,
		Content: strings.Repeat("y", 12_000),
	}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small and kept"},
		oversize,
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 0 || len(fold) != 2 {
		t.Fatalf("kept=%d fold=%d, want both turns folded", len(kept), len(fold))
	}
	if !strings.Contains(renderTranscript(fold), "small and kept") {
		t.Fatal("the small turn should be summarized with the oversize one")
	}
}

func TestPartitionMergesUserTurnsAcrossPriorDigest(t *testing.T) {
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "constraint from before the digest"},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nprior digest\n" + summaryTagClose},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 0 || !strings.Contains(renderTranscript(fold), "constraint from before") {
		t.Fatalf("pre-digest user turn must enter the merged summary; fold=%v", renderTranscript(fold))
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

func TestPartitionIgnoresDeprecatedKeepPolicy(t *testing.T) {
	a := &Agent{keepPolicy: KeepErrors}
	region := []provider.Message{
		{Role: provider.RoleAssistant, Content: "unrelated prose"},
		{Role: provider.RoleAssistant, Content: "call", ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "bash", Content: "error: boom"},
	}
	kept, fold := partitionCoversRegion(t, a, region)
	if len(kept) != 0 || len(fold) != 3 {
		t.Fatalf("kept=%d fold=%d, want every old message folded", len(kept), len(fold))
	}
	if got := renderTranscript(fold); !strings.Contains(got, "error: boom") || !strings.Contains(got, "call") {
		t.Fatalf("the failing call and its result must enter the summary together: %s", got)
	}
}
