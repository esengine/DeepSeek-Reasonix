package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type failingSummaryProvider struct{ calls int }

func (p *failingSummaryProvider) Name() string { return "failing-summary" }

func (p *failingSummaryProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("summary unavailable")}
	close(ch)
	return ch, nil
}

func TestContextManagerPersistsAndRestoresBlockedFailureFingerprint(t *testing.T) {
	// Above compact_ratio but below the physical hard ceiling: a failed summary
	// records a generation-scoped blocked receipt and does not reject the request.
	// Below hard, Prepare returns the uncompacted view rather than ErrCompactionRequired.
	const window = 10_000
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 500)},
		{Role: provider.RoleUser, Content: "current"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	newAgent := func(p *failingSummaryProvider) *Agent {
		a := New(p, tool.NewRegistry(), &Session{Messages: append([]provider.Message(nil), messages...)}, Options{
			ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2,
			WorkspaceID: "workspace", ModelRef: "model",
		}, event.Discard)
		a.BindSessionPath(path, true)
		return a
	}

	firstProvider := &failingSummaryProvider{}
	first := newAgent(firstProvider)
	// fold = 8500; hard = 9744. Observe between them so failure is non-fatal.
	policy := ContextPreparePolicy{Trigger: CompactionTriggerPressure, ObservedInputTokens: 8600}
	if _, err := first.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatalf("above-ratio failure should persist blocked state without rejecting this request: %v", err)
	}
	if firstProvider.calls != 1 { // single summary attempt; no summarizeOnce second pass
		t.Fatalf("summary calls = %d, want 1", firstProvider.calls)
	}
	if first.sess.compactionState.LastReceipt == nil {
		t.Fatal("failed summary did not persist a maintenance receipt")
	}
	if status := first.sess.compactionState.LastReceipt.Status; status != "blocked" && status != "failed" {
		t.Fatalf("receipt status = %q, want blocked or failed", status)
	}
	if first.sess.compactionState.LastReceipt.BlockedInputHash == "" {
		t.Fatal("failure receipt missing input hash")
	}
	if first.sess.compactionState.BlockedInputHash != "" {
		t.Fatalf("top-level blocked mirror should not be written: %q", first.sess.compactionState.BlockedInputHash)
	}
	if _, err := first.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if firstProvider.calls != 1 {
		t.Fatalf("same in-memory fingerprint retried summary: calls=%d", firstProvider.calls)
	}

	resumedProvider := &failingSummaryProvider{}
	resumed := newAgent(resumedProvider)
	if resumed.sess.compactionState.LastReceipt == nil {
		t.Fatal("failure receipt was not restored")
	}
	if status := resumed.sess.compactionState.LastReceipt.Status; status != "blocked" && status != "failed" {
		t.Fatalf("restored receipt status = %q, want blocked or failed", status)
	}
	if _, err := resumed.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if resumedProvider.calls != 0 {
		t.Fatalf("resumed blocked fingerprint retried summary %d times", resumedProvider.calls)
	}
}

// TestPrepareThresholdSkipsExtensionInterceptors locks the overflow-only
// contract: automatic compact_ratio uses the pre-interceptor request shape
// (messages + tools + role projection). context.prepare / provider.request
// interceptors run only on the real sampling path so side-effecting plugins
// are not double-invoked for threshold decisions.
func TestPrepareThresholdSkipsExtensionInterceptors(t *testing.T) {
	var prepareHits, providerHits atomic.Int32
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		switch ev {
		case protocol.EventContextPrepare:
			prepareHits.Add(1)
		case protocol.EventProviderRequest:
			providerHits.Add(1)
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointContextPrepare, extension.PointProviderRequest)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 50_000, CompactRatio: 0.85, RecentKeep: 2,
		Extensions: d, WorkspaceID: "ws", ModelRef: "m",
	}, event.Discard)

	// Below fold: Prepare sizes the view and must not touch interceptors.
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{
		Trigger: CompactionTriggerPressure, ObservedInputTokens: 100,
	}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepareHits.Load() != 0 || providerHits.Load() != 0 {
		t.Fatalf("threshold Prepare invoked interceptors: prepare=%d provider=%d",
			prepareHits.Load(), providerHits.Load())
	}

	// Real sampling assembly still runs both interceptor points once.
	if _, err := a.buildSamplingRequest(context.Background(), CompactionTriggerPressure); err != nil {
		t.Fatalf("buildSamplingRequest: %v", err)
	}
	if prepareHits.Load() != 1 || providerHits.Load() != 1 {
		t.Fatalf("sampling path interceptors: prepare=%d provider=%d, want 1 each",
			prepareHits.Load(), providerHits.Load())
	}
}

func TestStrictAlternatingRolesStillConvergesBeforeSampling(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 400)},
		{Role: provider.RoleUser, Content: "recent request"},
		{Role: provider.RoleAssistant, Content: "recent response"},
	}}
	a := New(&fakeProvider{reply: "old work summarized"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 200, RecentKeep: 2, StrictAlternatingRoles: true,
	}, event.Discard)

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	if got := a.currentProjectionVersion(); got != 1 {
		t.Fatalf("projection version = %d, want pressure fold", got)
	}
	if len(prepared.req.Messages) >= len(sess.Snapshot()) {
		t.Fatalf("strict request did not converge: %+v", prepared.req.Messages)
	}
	for i := 1; i < len(prepared.req.Messages); i++ {
		if prepared.req.Messages[i-1].Role == prepared.req.Messages[i].Role {
			t.Fatalf("strict request has adjacent %s roles: %+v", prepared.req.Messages[i].Role, prepared.req.Messages)
		}
	}
}
