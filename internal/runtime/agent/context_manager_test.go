package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reasonix/internal/state/sessionstore"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/protocol"
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
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 50_000, CompactRatio: 0.85, Extensions: d, WorkspaceID: "ws", ModelRef: "m",
	}, event.Discard)

	// Below fold: Prepare sizes the view and must not touch interceptors.
	if _, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{
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
