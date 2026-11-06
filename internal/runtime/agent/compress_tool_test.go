package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestCompressContextAfterExcludesActiveTurnAndAppendsToolResult(t *testing.T) {
	const activeCreatedAt = int64(99)
	currentCall := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "compress-1", Name: "compress", Arguments: `{}`}}}
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "start folding at alpha"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("completed work ", 180)},
		{Role: provider.RoleUser, Content: "another completed turn"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("more completed work ", 180)},
		{Role: provider.RoleUser, Content: "active request", CreatedAt: activeCreatedAt},
		currentCall,
	}}
	before := sess.Snapshot()
	a := New(&fakeProvider{reply: "completed turns summarized"}, tool.NewRegistry(), sess, Options{}, event.Discard)
	a.activeTurnCreatedAt.Store(activeCreatedAt)

	got, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "after", Anchor: "folding at alpha"})
	if err != nil {
		t.Fatalf("CompressContext: %v", err)
	}
	if got.Status != "ok" || got.Messages != 4 {
		t.Fatalf("result = %+v", got)
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatal("compress changed the active canonical turn")
	}
	visible := a.window().modelVisibleMessages()
	if !strings.Contains(joinContents(visible), "active request") || len(visible[len(visible)-1].ToolCalls) != 1 || visible[len(visible)-1].ToolCalls[0].ID != "compress-1" {
		t.Fatalf("active turn was not retained: %+v", visible)
	}

	toolResult := provider.Message{Role: provider.RoleTool, ToolCallID: "compress-1", Name: "compress", Content: `{"status":"ok"}`}
	sess.Add(toolResult)
	visible = a.window().modelVisibleMessages()
	if last := visible[len(visible)-1]; last.ToolCallID != "compress-1" || last.Content != toolResult.Content {
		t.Fatalf("post-projection tool result missing: %+v", visible)
	}
}

func TestCompressContextAnchorErrorsDoNotChangeState(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "shared phrase first"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "shared phrase second"},
	}}
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{}, event.Discard)
	before := sess.Snapshot()

	for _, tc := range []struct {
		anchor string
		want   string
	}{
		{anchor: "missing", want: "did not match"},
		{anchor: "shared phrase", want: "longer unique excerpt"},
	} {
		_, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: tc.anchor})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("anchor %q error = %v, want %q", tc.anchor, err, tc.want)
		}
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) || len(a.sess.win.compactionState.Projection.Messages) != 0 {
		t.Fatal("failed anchor lookup changed state")
	}
}

func TestCompressContextConsecutiveCallsMergeSummary(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "turn alpha"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("alpha work ", 180)},
		{Role: provider.RoleUser, Content: "turn beta unique"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("beta work ", 180)},
		{Role: provider.RoleUser, Content: "turn gamma unique"},
		{Role: provider.RoleAssistant, Content: "gamma tail"},
	}}
	before := sess.Snapshot()
	a := New(&fakeProvider{reply: "rolling summary"}, tool.NewRegistry(), sess, Options{StrictAlternatingRoles: true}, event.Discard)

	for _, anchor := range []string{"beta unique", "gamma unique"} {
		got, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: anchor})
		if err != nil || got.Status != "ok" {
			t.Fatalf("compress before %q = %+v, %v", anchor, got, err)
		}
	}
	visible := a.window().modelVisibleMessages()
	summaries := 0
	for _, msg := range visible {
		if isCompactionSummary(msg) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("summary count = %d, want 1: %+v", summaries, visible)
	}
	if !strings.Contains(joinContents(visible), "turn gamma unique") || !strings.Contains(joinContents(visible), "gamma tail") {
		t.Fatalf("unselected tail changed: %+v", visible)
	}
	if len(visible) < 3 || !isCompactionSummary(visible[1]) || visible[2].Content != "turn gamma unique" {
		t.Fatalf("projection lost logical user-turn boundary: %+v", visible)
	}
	providerView := a.providerProjectionMessages(visible)
	for i := 1; i < len(providerView); i++ {
		if providerView[i-1].Role == provider.RoleUser && providerView[i].Role == provider.RoleUser {
			t.Fatalf("strict provider view has adjacent user roles: %+v", providerView)
		}
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatal("consecutive compression changed canonical transcript")
	}
}

func TestCompressionVisibleMessagesSplitsLegacyStrictSummary(t *testing.T) {
	legacy := coalesceProjectionUserRuns([]provider.Message{
		formatSummaryMessage("prior facts"),
		{Role: provider.RoleUser, Content: "legacy retained anchor", Images: []string{"data:image/png;base64,AA=="}},
	})
	if len(legacy) != 1 {
		t.Fatalf("legacy setup did not coalesce: %+v", legacy)
	}
	visible := compressionVisibleMessages(legacy)
	if len(visible) != 2 || !isCompactionSummary(visible[0]) || !compressAnchorCandidate(visible[1]) {
		t.Fatalf("legacy strict summary was not split: %+v", visible)
	}
	if visible[1].Content != "legacy retained anchor" || len(visible[1].Images) != 1 || len(visible[0].Images) != 0 {
		t.Fatalf("legacy retained user payload changed: %+v", visible)
	}
}

func TestCompressContextNoSavingsIsNoop(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "tiny"},
		{Role: provider.RoleUser, Content: "keep boundary"},
	}}
	a := New(&fakeProvider{reply: strings.Repeat("long summary ", 30)}, tool.NewRegistry(), sess, Options{}, event.Discard)

	got, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: "keep boundary"})
	if err != nil {
		t.Fatalf("CompressContext: %v", err)
	}
	if got.Status != "noop" || !strings.Contains(got.Reason, "not be smaller") {
		t.Fatalf("result = %+v", got)
	}
	if len(a.sess.win.compactionState.Projection.Messages) != 0 {
		t.Fatal("noop installed a projection")
	}
	if reasons := sess.DrainContentRewriteReasons(); len(reasons) != 0 {
		t.Fatalf("noop reported cache rewrite reasons: %v", reasons)
	}
}
