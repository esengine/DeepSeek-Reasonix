package boundedllm

// This suite pins Call's fail-closed contracts directly: input budgets,
// default/custom limits, request shape, output cap, timeout, and the usage
// event emission rules. The recovery package exercises Call end-to-end through
// Session.Review; these tests hold the primitive's own guarantees so reviewer
// tests can trust the layer below them.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// fakeProvider records the request it receives and replays a pre-scripted
// chunk sequence. With no chunks it simulates a provider that only reacts to
// context cancellation — the shape used to exercise the Call timeout path.
type fakeProvider struct {
	chunks   []provider.Chunk
	startErr error
	req      provider.Request
	calls    int
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	p.req = req
	if p.startErr != nil {
		return nil, p.startErr
	}
	ch := make(chan provider.Chunk, len(p.chunks)+1)
	if len(p.chunks) == 0 {
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	}
	for _, c := range p.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// collectSink records every event it is handed.
type collectSink struct {
	events []event.Event
}

func (s *collectSink) Emit(e event.Event) { s.events = append(s.events, e) }

// textChunks scripts a normal stream: the given text deltas followed by a
// ChunkDone terminator.
func textChunks(parts ...string) []provider.Chunk {
	chunks := make([]provider.Chunk, 0, len(parts)+1)
	for _, part := range parts {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: part})
	}
	return append(chunks, provider.Chunk{Type: provider.ChunkDone})
}

func TestCallNilProviderFailsClosed(t *testing.T) {
	_, err := Call(context.Background(), Config{}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("err = %v, want provider unavailable", err)
	}
}

// A typed nil inside the Provider interface is still "not configured": the
// fail-closed check must not dereference it into a panic.
func TestCallTypedNilProviderFailsClosed(t *testing.T) {
	var p *fakeProvider
	_, err := Call(context.Background(), Config{Provider: p}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("err = %v, want provider unavailable", err)
	}
}

func TestCallRejectsOversizedInputs(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		system      string
		evidence    string
		wantErrPart string
	}{
		{
			name:        "system over custom cap",
			cfg:         Config{MaxSystemBytes: 10},
			system:      strings.Repeat("x", 64),
			wantErrPart: "system policy exceeds 10 bytes",
		},
		{
			name:        "system over default cap",
			system:      strings.Repeat("x", DefaultMaxSystemBytes+1),
			wantErrPart: "system policy exceeds",
		},
		{
			name:        "combined over custom total cap",
			cfg:         Config{MaxSystemBytes: 10, MaxTotalBytes: 20},
			system:      strings.Repeat("x", 8),
			evidence:    strings.Repeat("x", 13),
			wantErrPart: "request exceeds 20 bytes",
		},
		{
			name:        "combined over default total cap",
			system:      strings.Repeat("x", 2000),
			evidence:    strings.Repeat("x", DefaultMaxTotalBytes-2000+1),
			wantErrPart: "request exceeds",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &fakeProvider{}
			tt.cfg.Provider = prov
			_, err := Call(context.Background(), tt.cfg, tt.system, tt.evidence)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErrPart)
			}
			if prov.calls != 0 {
				t.Fatalf("provider calls = %d, want 0 (fail closed before any network call)", prov.calls)
			}
		})
	}
}

// The caps are checked with strict inequality: a request that exactly fills
// its budget must still be admitted.
func TestCallBudgetBoundariesPass(t *testing.T) {
	system := strings.Repeat("s", 10)
	evidence := strings.Repeat("e", 10) // system+evidence == MaxTotalBytes exactly
	prov := &fakeProvider{chunks: textChunks("ok")}
	text, err := Call(context.Background(), Config{
		Provider:       prov,
		MaxSystemBytes: 10,
		MaxTotalBytes:  20,
	}, system, evidence)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q, want %q", text, "ok")
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls)
	}
}

func TestCallAppliesDefaultLimits(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("ok")}
	text, err := Call(context.Background(), Config{Provider: prov}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q, want %q", text, "ok")
	}
	req := prov.req
	if req.MaxTokens != DefaultMaxTokens {
		t.Fatalf("max tokens = %d, want %d", req.MaxTokens, DefaultMaxTokens)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != provider.RoleSystem || req.Messages[0].Content != "sys" {
		t.Fatalf("system message = %+v, want fixed policy in role system", req.Messages[0])
	}
	if req.Messages[1].Role != provider.RoleUser || req.Messages[1].Content != "ev" {
		t.Fatalf("user message = %+v, want evidence in role user", req.Messages[1])
	}
	if len(req.Tools) != 0 {
		t.Fatalf("tools = %d, want none", len(req.Tools))
	}
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("temperature = %v, want explicit 0", req.Temperature)
	}
}

func TestCallAppliesCustomLimits(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("ok")}
	if _, err := Call(context.Background(), Config{Provider: prov, MaxTokens: 42}, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if prov.req.MaxTokens != 42 {
		t.Fatalf("max tokens = %d, want 42", prov.req.MaxTokens)
	}
}

func TestCallCollectsStreamedTextInOrder(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("a", "b", "c")}
	text, err := Call(context.Background(), Config{Provider: prov}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if text != "abc" {
		t.Fatalf("text = %q, want %q", text, "abc")
	}
}

func TestCallAbortsOnOutputOverflow(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks(strings.Repeat("x", 8), strings.Repeat("y", 8))}
	_, err := Call(context.Background(), Config{Provider: prov, MaxOutputBytes: 10}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "output exceeded 10 bytes") {
		t.Fatalf("err = %v, want output budget error", err)
	}
}

func TestCallOutputAtExactCapSucceeds(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("abcd")}
	text, err := Call(context.Background(), Config{Provider: prov, MaxOutputBytes: 4}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if text != "abcd" {
		t.Fatalf("text = %q, want %q", text, "abcd")
	}
}

func TestCallPropagatesStreamStartError(t *testing.T) {
	prov := &fakeProvider{startErr: errors.New("provider down")}
	_, err := Call(context.Background(), Config{Provider: prov}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("err = %v, want stream start error", err)
	}
}

func TestCallPropagatesChunkError(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{{Type: provider.ChunkError, Err: errors.New("mid-stream")}}}
	_, err := Call(context.Background(), Config{Provider: prov}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "mid-stream") {
		t.Fatalf("err = %v, want chunk error", err)
	}
}

// A ChunkError with no underlying error must still fail the call with a
// stable generic message rather than panic or succeed silently.
func TestCallChunkErrorWithoutErrIsGeneric(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{{Type: provider.ChunkError}}}
	_, err := Call(context.Background(), Config{Provider: prov}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "bounded reviewer stream error") {
		t.Fatalf("err = %v, want generic stream error", err)
	}
}

// A provider that neither responds nor closes its stream must surface the
// deadline once Timeout elapses — never hang the reviewer forever.
func TestCallTimeoutWithoutOutput(t *testing.T) {
	prov := &fakeProvider{} // no chunks: only reacts to ctx cancellation
	_, err := Call(context.Background(), Config{Provider: prov, Timeout: 50 * time.Millisecond}, "sys", "ev")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestCallEmitsUsageEvent(t *testing.T) {
	pricing := &provider.Pricing{Input: 1, Output: 2, Currency: "USD"}
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "verdict"},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15}},
		{Type: provider.ChunkDone},
	}}
	sink := &collectSink{}
	if _, err := Call(context.Background(), Config{
		Provider:    prov,
		Pricing:     pricing,
		ModelRef:    "deepseek/deepseek-v4-flash",
		Sink:        sink,
		UsageSource: event.UsageSourceGoalEvaluator,
	}, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Kind != event.Usage {
		t.Fatalf("kind = %v, want %v", ev.Kind, event.Usage)
	}
	if ev.UsageSource != event.UsageSourceGoalEvaluator || ev.Source != event.UsageSourceGoalEvaluator {
		t.Fatalf("usage source = %q, source = %q", ev.UsageSource, ev.Source)
	}
	if ev.ModelRef != "deepseek/deepseek-v4-flash" {
		t.Fatalf("model ref = %q", ev.ModelRef)
	}
	if ev.Pricing != pricing {
		t.Fatalf("pricing = %+v, want the configured instance", ev.Pricing)
	}
	if ev.Usage == nil || ev.Usage.PromptTokens != 12 || ev.Usage.CompletionTokens != 3 || ev.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", ev.Usage)
	}
}

// The emitted event must carry its own copy of usage: mutating the provider's
// record after the call must not rewrite what was already accounted.
func TestCallEmittedUsageIsACopy(t *testing.T) {
	usage := &provider.Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15}
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: usage},
		{Type: provider.ChunkDone},
	}}
	sink := &collectSink{}
	if _, err := Call(context.Background(), Config{
		Provider:    prov,
		Sink:        sink,
		UsageSource: event.UsageSourceGoalEvaluator,
	}, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	usage.TotalTokens = 999 // provider's record changes after the fact
	if sink.events[0].Usage == nil || sink.events[0].Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v, want an independent snapshot (total 15)", sink.events[0].Usage)
	}
	if sink.events[0].Usage == usage {
		t.Fatalf("emitted usage shares the provider's pointer")
	}
}

func TestCallSkipsUsageEventWithoutSource(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1}},
		{Type: provider.ChunkDone},
	}}
	sink := &collectSink{}
	if _, err := Call(context.Background(), Config{Provider: prov, Sink: sink}, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %d, want 0 (no UsageSource)", len(sink.events))
	}
}

func TestCallSkipsUsageEventWithoutUsageChunk(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("verdict")}
	sink := &collectSink{}
	if _, err := Call(context.Background(), Config{
		Provider:    prov,
		Sink:        sink,
		UsageSource: event.UsageSourceGoalEvaluator,
	}, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %d, want 0 (no usage reported by provider)", len(sink.events))
	}
}

// An output-budget abort must not leak a partial or phantom usage event: the
// provider never delivered usage before the cap tripped.
func TestCallOutputAbortEmitsNoUsageEvent(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: strings.Repeat("x", 16)},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1}},
		{Type: provider.ChunkDone},
	}}
	sink := &collectSink{}
	if _, err := Call(context.Background(), Config{
		Provider:       prov,
		Sink:           sink,
		UsageSource:    event.UsageSourceGoalEvaluator,
		MaxOutputBytes: 8,
	}, "sys", "ev"); err == nil {
		t.Fatal("want output budget error")
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %d, want 0 after abort", len(sink.events))
	}
}

func TestCallNilContextIsBackground(t *testing.T) {
	prov := &fakeProvider{chunks: textChunks("ok")}
	text, err := Call(nil, Config{Provider: prov}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if text != "ok" || prov.calls != 1 {
		t.Fatalf("text = %q, calls = %d", text, prov.calls)
	}
}
