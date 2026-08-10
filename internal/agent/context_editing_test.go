package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type nativeContextEditingProvider struct {
	*fakeProvider
}

func (p *nativeContextEditingProvider) ContextEditingCapabilities() provider.ContextEditingCapabilities {
	return provider.ContextEditingCapabilities{
		NativeToolUseClear: true,
		PolicyVersion:      "clear_tool_uses_test",
	}
}

type nativeFallbackProvider struct {
	mu           sync.Mutex
	rejectNative bool
	requestModes []string
}

func (p *nativeFallbackProvider) Name() string { return "native-fallback" }

func (p *nativeFallbackProvider) ContextEditingCapabilities() provider.ContextEditingCapabilities {
	return provider.ContextEditingCapabilities{
		NativeToolUseClear: true,
		PolicyVersion:      "clear_tool_uses_test",
	}
}

func (p *nativeFallbackProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	mode := "local"
	if req.ContextEditing != nil {
		mode = req.ContextEditing.Mode
	}
	p.mu.Lock()
	p.requestModes = append(p.requestModes, mode)
	reject := p.rejectNative && mode == "native"
	p.mu.Unlock()
	if reject {
		return nil, errors.Join(provider.ErrNativeContextEditingUnsupported, &provider.APIError{
			Provider: "native-fallback", Status: 400,
			Body: "context_management: extra inputs are not permitted",
		})
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *nativeFallbackProvider) setRejectNative(reject bool) {
	p.mu.Lock()
	p.rejectNative = reject
	p.mu.Unlock()
}

func (p *nativeFallbackProvider) modes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requestModes...)
}

func TestContextEditingResolvesProviderCapabilityBeforeRequestAndCacheLineage(t *testing.T) {
	opts := Options{
		ContextEditing:      "native",
		ContextWindow:       100_000,
		MaxOutputTokens:     1_024,
		WorkspaceID:         "workspace",
		ModelRef:            "model",
		ToolResultSnipRatio: 0.6,
	}
	sess := &Session{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}
	native := New(&nativeContextEditingProvider{fakeProvider: &fakeProvider{}}, tool.NewRegistry(), sess, opts, event.Discard)
	if native.requestedContextEditing != "native" || native.contextEditing != "native" {
		t.Fatalf("native modes = requested %q effective %q", native.requestedContextEditing, native.contextEditing)
	}
	prepared, err := native.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepare native request: %v", err)
	}
	policy := prepared.req.ContextEditing
	if policy == nil || policy.Mode != "native" || policy.KeepToolUses != 3 || policy.ClearToolInputs {
		t.Fatalf("native policy = %+v", policy)
	}
	if policy.TriggerInputTokens != 60_000 || policy.ClearAtLeastInputTokens != 4_096 {
		t.Fatalf("native thresholds = trigger %d min %d", policy.TriggerInputTokens, policy.ClearAtLeastInputTokens)
	}
	if key := native.currentPromptCacheKey(); !strings.Contains(key, "clear_tool_uses_test-t60000-k3-m4096-ifalse") {
		t.Fatalf("native cache lineage does not include the effective policy: %q", key)
	}

	unsupported := New(&fakeProvider{}, tool.NewRegistry(), NewSession(""), opts, event.Discard)
	localOpts := opts
	localOpts.ContextEditing = "local"
	local := New(&fakeProvider{}, tool.NewRegistry(), NewSession(""), localOpts, event.Discard)
	if unsupported.requestedContextEditing != "native" || unsupported.contextEditing != "local" {
		t.Fatalf("unsupported modes = requested %q effective %q", unsupported.requestedContextEditing, unsupported.contextEditing)
	}
	if unsupported.currentPromptCacheKey() != local.currentPromptCacheKey() {
		t.Fatalf("unsupported native split cache lineage: %q != local %q", unsupported.currentPromptCacheKey(), local.currentPromptCacheKey())
	}
	unsupportedPrepared, err := unsupported.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepare unsupported request: %v", err)
	}
	if unsupportedPrepared.req.ContextEditing != nil {
		t.Fatalf("unsupported provider received native policy: %+v", unsupportedPrepared.req.ContextEditing)
	}
}

func TestUnsupportedNativeContextEditingNoticeIsOneShot(t *testing.T) {
	sink := &recordSink{}
	a := New(&fakeProvider{}, tool.NewRegistry(), NewSession(""), Options{
		ContextEditing:  "native",
		ContextWindow:   100_000,
		MaxOutputTokens: 1_024,
	}, sink)
	for range 2 {
		if _, err := a.prepareSamplingRequest(context.Background()); err != nil {
			t.Fatalf("prepare request: %v", err)
		}
	}
	var notices int
	for _, got := range sink.kinds(event.Notice) {
		if got.Code == event.NoticeCodeContextEditingFallback {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("fallback notices = %d, want 1", notices)
	}
}

func TestNativeContextEditingFallsBackOnceAndPersistsLocalLineage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	prov := &nativeFallbackProvider{rejectNative: true}
	sess := &Session{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}
	opts := Options{
		ContextEditing: "native", ContextWindow: 100_000, MaxOutputTokens: 1_024,
		WorkspaceID: "workspace", ModelRef: "model",
	}
	a := New(prov, tool.NewRegistry(), sess, opts, event.Discard)
	a.BindSessionPath(path, false)

	got := a.streamWithSamplingRecovery(context.Background(), 1)
	if got.err != nil || got.text != "ok" {
		t.Fatalf("native fallback result = text %q err %v", got.text, got.err)
	}
	if modes := prov.modes(); len(modes) != 2 || modes[0] != "native" || modes[1] != "local" {
		t.Fatalf("request modes = %v, want [native local]", modes)
	}
	if a.effectiveContextEditing() != "local" || !a.contextEditingRuntimeFallback.Load() || a.nativeContextEditingAccepted.Load() {
		t.Fatalf("fallback state = effective %q fallback %t accepted %t",
			a.effectiveContextEditing(), a.contextEditingRuntimeFallback.Load(), a.nativeContextEditingAccepted.Load())
	}
	state, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load fallback sidecar: ok=%t err=%v", ok, err)
	}
	if !state.ContextEditingFallbackLocal || state.NativeContextEditingAccepted {
		t.Fatalf("persisted fallback state = %+v", state)
	}

	localOpts := opts
	localOpts.ContextEditing = "local"
	local := New(&fakeProvider{}, tool.NewRegistry(), NewSession(""), localOpts, event.Discard)
	local.SetSessionPath(path)
	if a.currentPromptCacheKey() != local.currentPromptCacheKey() {
		t.Fatalf("fallback lineage = %q, want local %q", a.currentPromptCacheKey(), local.currentPromptCacheKey())
	}

	resumed := New(&nativeFallbackProvider{}, tool.NewRegistry(), sess, opts, event.Discard)
	resumed.BindSessionPath(path, true)
	if resumed.effectiveContextEditing() != "local" || !resumed.contextEditingRuntimeFallback.Load() {
		t.Fatalf("resumed fallback = effective %q fallback %t",
			resumed.effectiveContextEditing(), resumed.contextEditingRuntimeFallback.Load())
	}
	prepared, err := resumed.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.req.ContextEditing != nil {
		t.Fatalf("resumed local request contains native policy: %+v", prepared.req.ContextEditing)
	}
}

func TestNativeContextEditingSuccessLatchesRequestShapeAcrossResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	prov := &nativeFallbackProvider{}
	sess := &Session{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}
	opts := Options{
		ContextEditing: "native", ContextWindow: 100_000, MaxOutputTokens: 1_024,
		WorkspaceID: "workspace", ModelRef: "model",
	}
	a := New(prov, tool.NewRegistry(), sess, opts, event.Discard)
	a.BindSessionPath(path, false)
	first := a.streamWithSamplingRecovery(context.Background(), 1)
	if first.err != nil || !a.nativeContextEditingAccepted.Load() {
		t.Fatalf("first native request = err %v accepted %t", first.err, a.nativeContextEditingAccepted.Load())
	}
	prov.setRejectNative(true)
	second := a.streamWithSamplingRecovery(context.Background(), 2)
	if !provider.IsNativeContextEditingUnsupported(second.err) {
		t.Fatalf("latched native rejection = %v, want unsupported error", second.err)
	}
	if modes := prov.modes(); len(modes) != 2 || modes[0] != "native" || modes[1] != "native" {
		t.Fatalf("latched request modes = %v, want [native native]", modes)
	}
	if a.effectiveContextEditing() != "native" || a.contextEditingRuntimeFallback.Load() {
		t.Fatalf("latched mode silently changed: effective %q fallback %t",
			a.effectiveContextEditing(), a.contextEditingRuntimeFallback.Load())
	}

	resumed := New(&nativeFallbackProvider{}, tool.NewRegistry(), sess, opts, event.Discard)
	resumed.BindSessionPath(path, true)
	if resumed.effectiveContextEditing() != "native" || !resumed.nativeContextEditingAccepted.Load() {
		t.Fatalf("resumed latch = effective %q accepted %t",
			resumed.effectiveContextEditing(), resumed.nativeContextEditingAccepted.Load())
	}
	prepared, err := resumed.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.req.ContextEditing == nil || prepared.req.ContextEditing.Mode != "native" {
		t.Fatalf("resumed native policy = %+v", prepared.req.ContextEditing)
	}
}

func TestNativeContextEditingReplacesLocalToolMaintenanceButKeepsFullFold(t *testing.T) {
	newAgent := func() (*Agent, *fakeProvider, *Session) {
		fake := &fakeProvider{reply: "summary"}
		sess := pruneFixture(strings.Repeat("line\n", 1000))
		a := New(&nativeContextEditingProvider{fakeProvider: fake}, tool.NewRegistry(), sess, Options{
			ContextEditing: "native",
			ContextWindow:  1_000,
			RecentKeep:     2,
			ArchiveDir:     t.TempDir(),
		}, event.Discard)
		return a, fake, sess
	}

	a, fake, sess := newAgent()
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 650})
	if fake.got != nil {
		t.Fatal("summarizer was called in the native tool-clear pressure band")
	}
	if got := a.currentProjectionVersion(); got != 0 {
		t.Fatalf("native mode installed a local snip projection, version = %d", got)
	}
	if got := sess.Snapshot()[3].Content; strings.HasPrefix(got, snippedMarker) || strings.HasPrefix(got, prunedMarker) {
		t.Fatalf("native mode rewrote canonical tool result: %.80q", got)
	}

	a, fake, _ = newAgent()
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 850})
	if fake.got == nil {
		t.Fatal("native mode suppressed the full-fold fallback above the fold trigger")
	}
}

func TestNativeContextEditingAppliedEditWritesUnifiedReceipt(t *testing.T) {
	sink := &recordSink{}
	a := New(&nativeContextEditingProvider{fakeProvider: &fakeProvider{}}, tool.NewRegistry(), NewSession("system"), Options{
		ContextEditing: "native", ContextWindow: 100_000,
	}, sink)
	a.contextManager().ObserveUsage(&provider.Usage{
		PromptTokens: 25_000, ContextEditingType: "clear_tool_uses_20250919",
		ContextEditingClearedToolUses: 8, ContextEditingClearedTokens: 50_000,
	})
	snapshot := a.ContextMaintenanceSnapshot()
	if snapshot.LastReceipt == nil || snapshot.LastReceipt.Action != "native_tool_clear" ||
		snapshot.LastReceipt.SavedTokens != 50_000 || snapshot.LastReceipt.AffectedToolResults != 8 {
		t.Fatalf("native maintenance receipt = %+v", snapshot.LastReceipt)
	}
	if got := sink.kinds(event.ContextMaintenanceEvent); len(got) != 1 || got[0].Maintenance.SavedTokens != 50_000 {
		t.Fatalf("native maintenance events = %+v", got)
	}
	// Duplicate observation for the same request is idempotent.
	a.contextManager().ObserveUsage(&provider.Usage{
		PromptTokens: 25_000, ContextEditingType: "clear_tool_uses_20250919",
		ContextEditingClearedToolUses: 8, ContextEditingClearedTokens: 50_000,
	})
	if got := len(sink.kinds(event.ContextMaintenanceEvent)); got != 1 {
		t.Fatalf("duplicate native maintenance events = %d, want 1", got)
	}
}

func TestTaskSubagentsPreserveRequestedContextEditingForTheirProvider(t *testing.T) {
	task := NewTaskToolWithOptions(TaskToolOptions{
		Provider:       &fakeProvider{},
		ParentRegistry: tool.NewRegistry(),
		ContextEditing: "native",
	})
	opts := task.subagentOptions(context.Background(), 5, nil, 100_000, 1, "child", nil)
	if opts.ContextEditing != "native" {
		t.Fatalf("subagent ContextEditing = %q, want requested native", opts.ContextEditing)
	}

	child := New(&fakeProvider{}, tool.NewRegistry(), NewSession(""), opts, event.Discard)
	if child.contextEditing != "local" {
		t.Fatalf("incompatible child effective mode = %q, want local", child.contextEditing)
	}
}
