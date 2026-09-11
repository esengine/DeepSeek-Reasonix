package boundedllm_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/boundedllm"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// fakeProvider streams a scripted chunk sequence and records the request it
// received so tests can assert on the bounded request shape.
type fakeProvider struct {
	name      string
	chunks    []provider.Chunk
	streamErr error

	gotCtx context.Context
	gotReq provider.Request
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.gotCtx = ctx
	f.gotReq = req
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan provider.Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func textChunk(s string) provider.Chunk { return provider.Chunk{Type: provider.ChunkText, Text: s} }

// sink records every emitted event.
type sink struct{ events []event.Event }

func (s *sink) Emit(e event.Event) { s.events = append(s.events, e) }

func TestCallNilProvider(t *testing.T) {
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{}, "sys", "ev"); err == nil {
		t.Fatal("expected error for nil provider, got nil")
	}
	var typedNil *fakeProvider
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: typedNil}, "sys", "ev"); err == nil {
		t.Fatal("expected error for typed-nil provider, got nil")
	}
}

func TestCallNilContext(t *testing.T) {
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{textChunk("ok")}}
	got, err := boundedllm.Call(nil, boundedllm.Config{Provider: fp}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call with nil ctx: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want %q", got, "ok")
	}
}

func TestCallSuccessAndRequestShape(t *testing.T) {
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{textChunk("hello "), textChunk("world")}}
	got, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}

	req := fp.gotReq
	if len(req.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != provider.RoleSystem || req.Messages[0].Content != "sys" {
		t.Errorf("system message = %+v, want role system with %q", req.Messages[0], "sys")
	}
	if req.Messages[1].Role != provider.RoleUser || req.Messages[1].Content != "ev" {
		t.Errorf("user message = %+v, want role user with %q", req.Messages[1], "ev")
	}
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", req.Temperature)
	}
	if req.MaxTokens != boundedllm.DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want default %d", req.MaxTokens, boundedllm.DefaultMaxTokens)
	}
	if req.Tools != nil {
		t.Errorf("tools = %v, want none", req.Tools)
	}
}

func TestCallCustomLimits(t *testing.T) {
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{textChunk("hi")}}
	cfg := boundedllm.Config{
		Provider:       fp,
		Timeout:        5 * time.Second,
		MaxTokens:      123,
		MaxOutputBytes: 1 << 20,
		MaxSystemBytes: 1 << 20,
		MaxTotalBytes:  2 << 20,
	}
	if _, err := boundedllm.Call(t.Context(), cfg, "sys", "ev"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if fp.gotReq.MaxTokens != 123 {
		t.Errorf("MaxTokens = %d, want 123", fp.gotReq.MaxTokens)
	}
}

func TestCallUsageEventEmitted(t *testing.T) {
	s := &sink{}
	pricing := &provider.Pricing{}
	usage := &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, FinishReason: "stop"}
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		textChunk("a"),
		{Type: provider.ChunkUsage, Usage: usage},
		textChunk("b"),
	}}
	_, err := boundedllm.Call(t.Context(), boundedllm.Config{
		Provider:    fp,
		Pricing:     pricing,
		ModelRef:    "fake/model",
		Sink:        s,
		UsageSource: event.UsageSourceGoalEvaluator,
	}, "sys", "ev")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(s.events) != 1 {
		t.Fatalf("got %d events, want 1", len(s.events))
	}
	e := s.events[0]
	if e.Kind != event.Usage {
		t.Errorf("Kind = %v, want %v", e.Kind, event.Usage)
	}
	if e.ModelRef != "fake/model" || e.UsageSource != event.UsageSourceGoalEvaluator || e.Source != event.UsageSourceGoalEvaluator {
		t.Errorf("event attribution = %+v, want model fake/model source goal-evaluator", e)
	}
	if e.Usage == nil || e.Usage.PromptTokens != 10 || e.Usage.CompletionTokens != 5 || e.Usage.TotalTokens != 15 {
		t.Errorf("usage = %+v, want 10/5/15 tokens", e.Usage)
	}
	if e.Pricing != pricing {
		t.Errorf("pricing not propagated")
	}
}

func TestCallUsageWithoutSinkOrSource(t *testing.T) {
	// No sink: usage must not panic and no event can be emitted.
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 3}},
		textChunk("a"),
	}}
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, UsageSource: "src"}, "sys", "ev"); err != nil {
		t.Fatalf("Call without sink: %v", err)
	}
	// Sink but empty UsageSource: no billable event.
	s := &sink{}
	fp = &fakeProvider{name: "fake", chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 3}},
	}}
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, Sink: s}, "sys", "ev"); err != nil {
		t.Fatalf("Call without source: %v", err)
	}
	if len(s.events) != 0 {
		t.Fatalf("got %d events, want 0", len(s.events))
	}
}

func TestCallStreamError(t *testing.T) {
	want := errors.New("boom")
	fp := &fakeProvider{name: "fake", streamErr: want}
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp}, "sys", "ev"); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestCallChunkError(t *testing.T) {
	want := errors.New("stream exploded")
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		textChunk("partial"),
		{Type: provider.ChunkError, Err: want},
	}}
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp}, "sys", "ev"); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestCallChunkErrorNil(t *testing.T) {
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{{Type: provider.ChunkError}}}
	if _, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp}, "sys", "ev"); err == nil || !strings.Contains(err.Error(), "stream error") {
		t.Fatalf("got %v, want stream error", err)
	}
}

func TestCallOutputTooLarge(t *testing.T) {
	fp := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		textChunk("hello "),
		textChunk("world"),
	}}
	_, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, MaxOutputBytes: 10}, "sys", "ev")
	if err == nil || !strings.Contains(err.Error(), "output exceeded") {
		t.Fatalf("got %v, want output exceeded error", err)
	}
}

func TestCallSystemTooLarge(t *testing.T) {
	fp := &fakeProvider{name: "fake"}
	_, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, MaxSystemBytes: 4}, "sys-bigger-than-4", "ev")
	if err == nil || !strings.Contains(err.Error(), "system policy exceeds") {
		t.Fatalf("got %v, want system policy exceeds error", err)
	}
}

func TestCallTotalTooLarge(t *testing.T) {
	fp := &fakeProvider{name: "fake"}
	_, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, MaxTotalBytes: 10}, "12345", "123456") // 5+6 > 10
	if err == nil || !strings.Contains(err.Error(), "request exceeds") {
		t.Fatalf("got %v, want request exceeds error", err)
	}
}

func TestCallTimeoutNoOutput(t *testing.T) {
	fp := &blockingProvider{}
	_, err := boundedllm.Call(t.Context(), boundedllm.Config{Provider: fp, Timeout: 50 * time.Millisecond}, "sys", "ev")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want %v", err, context.DeadlineExceeded)
	}
}

// blockingProvider never produces output and only closes its channel once the
// context is done, exercising the timeout-abort path.
type blockingProvider struct{}

func (b *blockingProvider) Name() string { return "blocking" }

func (b *blockingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}
