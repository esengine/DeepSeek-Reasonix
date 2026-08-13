package agent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCompressContextBeforePreservesCanonicalAndTail(t *testing.T) {
	large := strings.Repeat("old tool output ", 160)
	local := provider.Message{Role: provider.RoleTool, LocalOnly: true, Content: "private interrupted output"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system stays"},
		{Role: provider.RoleUser, Content: "old request alpha"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("analysis ", 160)},
		local,
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{"path":"a"}`}}},
		{Role: provider.RoleTool, ToolCallID: "read-1", Name: "read_file", Content: large},
		{Role: provider.RoleUser, Content: "unique boundary request"},
		{Role: provider.RoleAssistant, Content: "tail stays byte-for-byte"},
	}}
	before := sess.Snapshot()
	prov := &fakeProvider{reply: "old work summarized"}
	a := New(prov, tool.NewRegistry(), sess, Options{ArchiveDir: t.TempDir()}, event.Discard)

	got, err := a.CompressContext(context.Background(), tool.CompressRequest{
		Direction: "before", Anchor: "unique boundary", Focus: "keep file decisions",
	})
	if err != nil {
		t.Fatalf("CompressContext: %v", err)
	}
	if got.Status != "ok" || got.Direction != "before" || got.Messages != 4 || got.Mode != CompactionModeSummarized {
		t.Fatalf("result = %+v", got)
	}
	if got.ProjectionTokens >= got.SourceTokens {
		t.Fatalf("projection did not shrink: %+v", got)
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatal("compress changed the canonical transcript")
	}
	visible := a.modelVisibleMessages()
	if visible[0].Role != provider.RoleSystem || visible[0].Content != "system stays" {
		t.Fatalf("system message changed: %+v", visible)
	}
	if !hasCompactionSummary(visible) || !strings.Contains(joinContents(visible), "unique boundary request") || !strings.Contains(joinContents(visible), "tail stays byte-for-byte") {
		t.Fatalf("projection lost retained tail: %+v", visible)
	}
	if strings.Contains(joinContents(visible), large) || strings.Contains(joinContents(visible), local.Content) {
		t.Fatalf("projection retained folded/local-only content: %+v", visible)
	}
	if len(prov.got) < 2 || strings.Contains(prov.got[1].Content, local.Content) {
		t.Fatalf("LocalOnly content reached summarizer: %+v", prov.got)
	}
	state := a.sess.compactionState
	if state.Generation != 1 || state.Projection.ViewInputHash == "" || state.Projection.ViewOutputHash == "" {
		t.Fatalf("range compression did not install complete v3 lineage: %+v", state)
	}
	if state.LastReceipt == nil || state.LastReceipt.Status != "applied" || state.LastReceipt.Action != "summary" ||
		state.LastReceipt.Trigger != CompactionTriggerTool {
		t.Fatalf("range compression receipt = %+v", state.LastReceipt)
	}
	// New summary checkpoints do not create archives; full originals stay in canonical.
	if state.LastReceipt.Archive != "" {
		t.Fatalf("summary checkpoint should not create archive, got %q", state.LastReceipt.Archive)
	}
}

func TestCompressContextAfterExcludesActiveTurnAndAppendsToolResult(t *testing.T) {
	const activeCreatedAt = int64(99)
	currentCall := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "compress-1", Name: "compress", Arguments: `{}`}}}
	sess := &Session{Messages: []provider.Message{
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
	visible := a.modelVisibleMessages()
	if !strings.Contains(joinContents(visible), "active request") || len(visible[len(visible)-1].ToolCalls) != 1 || visible[len(visible)-1].ToolCalls[0].ID != "compress-1" {
		t.Fatalf("active turn was not retained: %+v", visible)
	}

	toolResult := provider.Message{Role: provider.RoleTool, ToolCallID: "compress-1", Name: "compress", Content: `{"status":"ok"}`}
	sess.Add(toolResult)
	visible = a.modelVisibleMessages()
	if last := visible[len(visible)-1]; last.ToolCallID != "compress-1" || last.Content != toolResult.Content {
		t.Fatalf("post-projection tool result missing: %+v", visible)
	}
}

func TestCompressContextAnchorErrorsDoNotChangeState(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
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
	if !reflect.DeepEqual(sess.Snapshot(), before) || len(a.sess.compactionState.Projection.Messages) != 0 {
		t.Fatal("failed anchor lookup changed state")
	}
}

func TestCompressContextConsecutiveCallsMergeSummary(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
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
	visible := a.modelVisibleMessages()
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
	sess := &Session{Messages: []provider.Message{
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
	if len(a.sess.compactionState.Projection.Messages) != 0 {
		t.Fatal("noop installed a projection")
	}
	if reasons := sess.DrainContentRewriteReasons(); len(reasons) != 0 {
		t.Fatalf("noop reported cache rewrite reasons: %v", reasons)
	}
}

func TestCompressContextFailureDoesNotArchiveUncommittedRange(t *testing.T) {
	archiveDir := t.TempDir()
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old unique"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("work ", 200)},
		{Role: provider.RoleUser, Content: "keep unique"},
	}}
	a := New(&fakeProvider{streamErr: errors.New("summary unavailable")}, tool.NewRegistry(), sess, Options{ArchiveDir: archiveDir}, event.Discard)

	if _, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: "keep unique"}); err == nil {
		t.Fatal("CompressContext succeeded with a failed summarizer")
	}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed range compression left %d archive files", len(entries))
	}
	if a.sess.compactionState.Generation != 0 || a.sess.compactionState.LastReceipt != nil {
		t.Fatalf("failed range compression changed sidecar state: %+v", a.sess.compactionState)
	}
}

type staleCompressProvider struct {
	started chan struct{}
	release chan struct{}
}

type singleflightCompressProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *singleflightCompressProvider) Name() string { return "singleflight-compress" }

func (p *singleflightCompressProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	n := p.calls.Add(1)
	ch := make(chan provider.Chunk, 2)
	if n == 1 {
		close(p.started)
		go func() {
			<-p.release
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "summary"}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			close(ch)
		}()
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "duplicate summary"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *staleCompressProvider) Name() string { return "stale-compress" }

func (p *staleCompressProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	close(p.started)
	go func() {
		<-p.release
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "summary"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
	}()
	return ch, nil
}

func TestCompressContextRejectsStaleTranscript(t *testing.T) {
	prov := &staleCompressProvider{started: make(chan struct{}), release: make(chan struct{})}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old unique"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("work ", 200)},
		{Role: provider.RoleUser, Content: "keep unique"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	errCh := make(chan error, 1)
	go func() {
		_, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: "keep unique"})
		errCh <- err
	}()
	<-prov.started
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "concurrent append"})
	close(prov.release)
	if err := <-errCh; !errors.Is(err, errCompressStaleContext) {
		t.Fatalf("error = %v, want stale context", err)
	}
	if len(a.sess.compactionState.Projection.Messages) != 0 {
		t.Fatal("stale compression installed a projection")
	}
	if reasons := sess.DrainContentRewriteReasons(); len(reasons) != 0 {
		t.Fatalf("stale compression reported cache rewrite reasons: %v", reasons)
	}
}

func TestRangeCompressionSharesSummarySingleflight(t *testing.T) {
	prov := &singleflightCompressProvider{started: make(chan struct{}), release: make(chan struct{})}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old unique"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("work ", 200)},
		{Role: provider.RoleUser, Content: "keep unique"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	autoErr := make(chan error, 1)
	go func() {
		_, err := a.compactToProjection(context.Background(), CompactionTriggerPressure, "", true, false)
		autoErr <- err
	}()
	<-prov.started

	snap := a.snapshotExplicitCompression()
	anchor := -1
	for i, msg := range snap.visible {
		if strings.Contains(UserMessageText(msg), "keep unique") {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		t.Fatal("range anchor missing from snapshot")
	}
	rangeErr := make(chan error, 1)
	go func() {
		_, err := a.compressVisibleRange(context.Background(), snap, CompactionTriggerTool, "before", anchor, "keep unique", "")
		rangeErr <- err
	}()
	close(prov.release)

	if err := <-autoErr; err != nil {
		t.Fatalf("automatic compression: %v", err)
	}
	if err := <-rangeErr; !errors.Is(err, errCompressStaleContext) {
		t.Fatalf("queued range compression error = %v, want stale context", err)
	}
	if got := prov.calls.Load(); got != 1 {
		t.Fatalf("summary provider calls = %d, want one shared transaction", got)
	}
}
