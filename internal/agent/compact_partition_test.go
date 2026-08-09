package agent

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// bigTurn is comfortably over maxPinnedFirstUserTokens, so it is never pinnable.
func bigTurn() string { return strings.Repeat("paste ", 4000) }

// partitionCoversRegion is the invariant that makes a lost user turn impossible:
// every provider-visible message in the region lands in exactly one group.
func partitionCoversRegion(t *testing.T, a *Agent, region []provider.Message) (early, kept, fold []provider.Message) {
	t.Helper()
	early, carried, kept, fold := a.partitionFoldForProjection(region)
	kept = append(carried, kept...)
	seen := map[string]int{}
	for _, group := range [][]provider.Message{early, kept, fold} {
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
	return early, kept, fold
}

func TestPartitionHoistsFirstSmallUserTurnsInOrder(t *testing.T) {
	// The hoist window is position-fixed (the first N), never "the most recent
	// N": a window that moved with each fold would rewrite the projection prefix
	// and crater the server-side cache.
	a := &Agent{}
	var region []provider.Message
	for i := range 25 {
		region = append(region, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("small turn %d", i)})
	}
	early, kept, fold := partitionCoversRegion(t, a, region)
	if len(early) != maxEarlyUserTurns || len(kept) != 0 || len(fold) != 25-maxEarlyUserTurns {
		t.Fatalf("early=%d kept=%d fold=%d, want %d/0/%d", len(early), len(kept), len(fold), maxEarlyUserTurns, 25-maxEarlyUserTurns)
	}
	for i, m := range early {
		if want := fmt.Sprintf("small turn %d", i); UserMessageText(m) != want {
			t.Fatalf("early[%d]=%q, want %q — the hoist window must be the leading turns", i, UserMessageText(m), want)
		}
	}
	for i, m := range fold {
		if want := fmt.Sprintf("small turn %d", maxEarlyUserTurns+i); UserMessageText(m) != want {
			t.Fatalf("fold[%d]=%q, want %q", i, UserMessageText(m), want)
		}
	}
}

func TestPartitionFoldsLargeUserTurns(t *testing.T) {
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: bigTurn()},
		{Role: provider.RoleUser, Content: "small"},
	}
	early, _, fold := partitionCoversRegion(t, a, region)
	if len(early) != 1 || UserMessageText(early[0]) != "small" {
		t.Fatalf("early=%+v, want only the small turn hoisted", early)
	}
	if len(fold) != 1 {
		t.Fatalf("fold=%d, want the large turn folded", len(fold))
	}
}

// A user turn that is not hoisted must still reach the summarizer. Two rules
// that disagreed on which turns count as "early" used to drop the turns after an
// oversized one from both groups, deleting stated constraints outright.
func TestPartitionKeepsSmallTurnsAfterALargeOne(t *testing.T) {
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "constraint A"},
		{Role: provider.RoleUser, Content: bigTurn()},
		{Role: provider.RoleUser, Content: "never touch the db schema"},
		{Role: provider.RoleUser, Content: "constraint C"},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	early, _, fold := partitionCoversRegion(t, a, region)
	if len(early) != 3 {
		t.Fatalf("early=%d (%+v), want the three small turns hoisted across the oversized one", len(early), early)
	}
	if got := renderTranscript(fold); !strings.Contains(got, "paste") {
		t.Fatalf("the oversized turn must still reach the summarizer:\n%s", got)
	}
}

// The hoist set is drawn from the fold region only. Reading past it would hoist
// a turn that also rides in the verbatim tail, sending it to the model twice.
func TestPartitionHoistIsDisjointFromTail(t *testing.T) {
	a := &Agent{}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleUser, Content: "constraint A"},
		{Role: provider.RoleAssistant, Content: "work"},
		{Role: provider.RoleUser, Content: "tail turn one"},
		{Role: provider.RoleAssistant, Content: "more"},
		{Role: provider.RoleUser, Content: "tail turn two"},
	}
	head := a.pinnedPrefixLen(msgs)
	const start = 4
	early, _, _ := partitionCoversRegion(t, a, msgs[head:start])
	for _, m := range early {
		for _, tail := range msgs[start:] {
			if m.Content == tail.Content {
				t.Fatalf("hoisted turn %q also rides in the verbatim tail", m.Content)
			}
		}
	}
}

func TestPartitionFoldsPriorDigests(t *testing.T) {
	// Any digest in the transcript is merged into the next one, so the model
	// never sees a chain of them.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nolder digest\n" + summaryTagClose},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\nnewer digest\n" + summaryTagClose},
		{Role: provider.RoleAssistant, Content: "work"},
	}
	early, kept, fold := partitionCoversRegion(t, a, region)
	if len(early) != 0 || len(kept) != 0 || len(fold) != 3 {
		t.Fatalf("early=%d kept=%d fold=%d, want every digest folded", len(early), len(kept), len(fold))
	}
}

func TestPartitionKeepPolicyOutranksFold(t *testing.T) {
	a := &Agent{keepPolicy: KeepErrors}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "small"},
		{Role: provider.RoleAssistant, Content: "call", ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "bash", Content: "error: boom"},
	}
	early, kept, fold := partitionCoversRegion(t, a, region)
	if len(early) != 1 || len(kept) != 2 || len(fold) != 0 {
		t.Fatalf("early=%d kept=%d fold=%d, want the error result and its caller kept", len(early), len(kept), len(fold))
	}
}
