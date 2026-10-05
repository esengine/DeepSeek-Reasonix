package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func armedAgent(t *testing.T, sess *sessionstore.Session, arm ablation.Set) *Agent {
	t.Helper()
	return New(&fakeProvider{reply: "Work continued."}, tool.NewRegistry(), sess, Options{
		ContextWindow: 5000, CompactRatio: 0.5, RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t), Ablation: arm,
	}, event.Discard)
}

// indexHeavySession makes far more index lines than any arm's budget can hold,
// so the four scales are separated by what they trim rather than by what the
// fixture happened to produce.
func indexHeavySession(calls int) *sessionstore.Session {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "sweep the tree"},
	}
	for i := range calls {
		id := fmt.Sprintf("c%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"internal/pkg%03d/service.go"}`, i)},
			}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: strings.Repeat("line ", 200)},
		)
	}
	return &sessionstore.Session{Messages: msgs}
}

// The axis has to reach the index, not just the arm name: a scale that changes
// nothing measures nothing.
func TestFoldIndexScaleChangesWhatTheModelSees(t *testing.T) {
	sizes := map[ablation.FoldIndexScale]int{}
	for _, scale := range []ablation.FoldIndexScale{
		ablation.FoldIndexDefault, ablation.FoldIndexHalf, ablation.FoldIndexQuarter, ablation.FoldIndexOff,
	} {
		a := New(&fakeProvider{reply: "Swept."}, tool.NewRegistry(), indexHeavySession(400), Options{
			ContextWindow: 120000, CompactRatio: 0.5, RecentKeep: 2,
			ArchiveDir: testenv.TempDir(t), Ablation: ablation.Set{}.WithFoldIndex(scale),
		}, event.Discard)
		if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
			t.Fatalf("scale %q prepare = %v", scale, err)
		}
		canonical, _ := a.sess.conversation.SnapshotMessagesVersion()
		lines := 0
		for _, m := range modelVisibleFromProjection(a.sess.win.compactionState.Projection, canonical) {
			if _, index := splitFoldIndex(m.Content); index != "" {
				lines += len(indexBodyLines(index))
			}
		}
		sizes[scale] = lines
		t.Logf("scale=%-7q budget=%4d index lines=%d", scale, a.window().foldIndexBudget(), lines)
	}
	if sizes[ablation.FoldIndexOff] != 0 {
		t.Errorf("the off arm still published %d index lines", sizes[ablation.FoldIndexOff])
	}
	if sizes[ablation.FoldIndexDefault] <= sizes[ablation.FoldIndexQuarter] {
		t.Errorf("quarter (%d lines) did not shrink the default (%d lines); the axis is inert",
			sizes[ablation.FoldIndexQuarter], sizes[ablation.FoldIndexDefault])
	}
	// Monotone, and each step separated: an arm that lands on the same index as
	// its neighbour measures the same thing twice.
	if sizes[ablation.FoldIndexHalf] >= sizes[ablation.FoldIndexDefault] {
		t.Errorf("half (%d) did not shrink the default (%d)", sizes[ablation.FoldIndexHalf], sizes[ablation.FoldIndexDefault])
	}
	if sizes[ablation.FoldIndexQuarter] >= sizes[ablation.FoldIndexHalf] {
		t.Errorf("quarter (%d) did not shrink half (%d)", sizes[ablation.FoldIndexQuarter], sizes[ablation.FoldIndexHalf])
	}
}

// An arm without search must not be told about it anywhere: not by the tool it
// holds, and not by the line the index writes when it drops an address.
func TestRecallSearchArmIsToldNothingItCannotUse(t *testing.T) {
	sess, _ := horizonSession(retrievalCarriers()[0], 6)
	a := armedAgent(t, sess, ablation.New(ablation.RecallSearch))
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if hint := a.window().droppedIndexHint(); strings.Contains(hint, "query") {
		t.Errorf("the dropped-entries line offers a query to an arm without search: %q", hint)
	}
	if _, err := a.RecallContext(context.Background(), tool.RecallRequest{Query: "timeout ownership"}); err == nil {
		t.Fatal("search ran in an arm that ablated it")
	}
	// The read half is untouched: an address the index still carries stays
	// readable, which is what the pre-search tool could always do.
	if _, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{2}}); err != nil {
		t.Errorf("read = %v, want the read half to keep working", err)
	}
}

// The control arm keeps both halves.
func TestControlArmKeepsSearch(t *testing.T) {
	sess, at := horizonSession(retrievalCarriers()[0], 6)
	a := armedAgent(t, sess, ablation.Set{})
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if hint := a.window().droppedIndexHint(); !strings.Contains(hint, "query") {
		t.Errorf("the control arm's dropped-entries line does not mention search: %q", hint)
	}
	res, err := a.RecallContext(context.Background(), tool.RecallRequest{
		Query: "timeout ownership controller middleware cancellation",
	})
	if err != nil {
		t.Fatalf("search = %v", err)
	}
	if hitRank(res, at) == 0 {
		t.Errorf("control arm search missed #%d: %v", at, hitPositions(res))
	}
}
