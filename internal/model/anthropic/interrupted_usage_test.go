package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/boundedllm"
)

const partialUsageStart = `data: {"type":"message_start","message":{"usage":{"input_tokens":1000,"output_tokens":2,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"server_tool_use":{"web_search_requests":1}}}}` + "\n\n"
const partialUsageDelta = `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":900,"output_tokens":7,"cache_creation_input_tokens":10,"cache_read_input_tokens":25}}` + "\n\n"
const partialUsageText = `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}` + "\n\n"

func TestInterruptedUsageTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, tail                    string
		text, complete, missing, zero bool
	}{
		{name: "upstream_error", tail: overloadedEvent},
		{name: "payload_error", tail: overloadedEvent, text: true},
		{name: "premature_eof"},
		{name: "decode_error", tail: "data: {bad\n\n"},
		{name: "reported_zero", zero: true},
		{name: "missing_control", missing: true},
		{name: "complete_control", complete: true, tail: "data: {\"type\":\"message_stop\"}\n\ndata: {bad\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wire strings.Builder
			if !tc.missing {
				if tc.zero {
					wire.WriteString("data: {\"type\":\"message_start\",\"message\":{\"usage\":{}}}\n\n")
				} else {
					wire.WriteString(partialUsageStart + partialUsageDelta + partialUsageDelta)
				}
			}
			if tc.text {
				wire.WriteString(partialUsageText)
			}
			wire.WriteString(tc.tail)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, wire.String())
			}))
			t.Cleanup(srv.Close)
			p, err := New(provider.Config{Name: "fixture", BaseURL: srv.URL, Model: "fixture", APIKey: "test"})
			if err != nil {
				t.Fatal(err)
			}
			ctx := provider.WithRequestAttemptCounter(t.Context())
			ch, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			var usage *provider.Usage
			var terminal error
			var usages, dones int
			for chunk := range ch {
				switch chunk.Type {
				case provider.ChunkUsage:
					if terminal != nil {
						t.Fatal("usage arrived after error")
					}
					usage = chunk.Usage
					usages++
				case provider.ChunkError:
					terminal = chunk.Err
				case provider.ChunkDone:
					dones++
				}
			}
			if tc.missing {
				if usage != nil || usages != 0 || terminal == nil {
					t.Fatalf("missing usage=%+v count=%d err=%v", usage, usages, terminal)
				}
				return
			}
			if usages != 1 || usage == nil || usage.RequestCount != 1 || usage.Estimated == tc.complete {
				t.Fatalf("usage=%+v count=%d", usage, usages)
			}
			if !tc.zero && (usage.PromptTokens != 1050 || usage.CompletionTokens != 7 || usage.TotalTokens != 1057 || usage.CacheHitTokens != 30 || usage.CacheMissTokens != 1020 || usage.CacheWriteTokens != 20 || usage.ServerToolRequests != 1) {
				t.Fatalf("cumulative wire counters lost or added twice: %+v", usage)
			}
			if tc.zero && usage.TotalTokens != 0 {
				t.Fatalf("invented tokens: %+v", usage)
			}
			if tc.complete {
				if terminal != nil || dones != 1 || usage.FinishReason != "stop" {
					t.Fatalf("committed terminal usage=%+v done=%d err=%v", usage, dones, terminal)
				}
			} else if terminal == nil || dones != 0 || usage.FinishReason != "interrupted" {
				t.Fatalf("interruption certified as complete: usage=%+v done=%d err=%v", usage, dones, terminal)
			}
			if tc.tail == overloadedEvent {
				var payload *provider.StreamPayloadError
				if !errors.As(terminal, &payload) || payload.Type != "overloaded_error" || provider.IsStreamInterrupted(terminal) == tc.text {
					t.Fatalf("usage changed original retry/payload classification: %v", terminal)
				}
			}
		})
	}
}

func TestInterruptedUsageBoundedConsumer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, partialUsageStart+partialUsageDelta+overloadedEvent)
	}))
	t.Cleanup(srv.Close)
	p, err := New(provider.Config{Name: "fixture", BaseURL: srv.URL, Model: "fixture", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var observed, reported *provider.Usage
	var observeCalls, reportCalls int
	ctx := provider.WithAuxiliaryUsageObserver(t.Context(), func(u *provider.Usage) { observed = u; observeCalls++ })
	_, err = boundedllm.Call(ctx, boundedllm.Config{Provider: p, UsageSource: event.UsageSourceAdvisor, Sink: event.FuncSink(func(e event.Event) { reported = e.Usage; reportCalls++ })}, "policy", "evidence")
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("lost interruption: %v", err)
	}
	if observeCalls != 1 || reportCalls != 1 || observed == nil || reported == nil || observed.PromptTokens != 1050 || reported.CompletionTokens != 7 || !reported.Estimated || observed.RequestCount != 1 {
		t.Fatalf("observer=%+v reporter=%+v calls=%d/%d", observed, reported, observeCalls, reportCalls)
	}
}

type interruptedUsageBody struct {
	io.Reader
	end error
}

func (b interruptedUsageBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		err = b.end
	}
	return n, err
}
func (interruptedUsageBody) Close() error { return nil }

func TestInterruptedUsageReadErrorAndCancellation(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "read_error", true: "cancelled_buffered_reader"}[cancelFirst], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("fixture body cut")
			body := interruptedUsageBody{Reader: strings.NewReader(partialUsageStart), end: cause}
			if cancelFirst {
				cancel()
			}
			out := make(chan provider.Chunk, 4)
			(&client{name: "fixture", nativeAnthropic: true}).readStream(ctx, &http.Response{Body: body}, out)
			var usage *provider.Usage
			var terminal error
			for chunk := range out {
				if chunk.Type == provider.ChunkUsage {
					if usage != nil {
						t.Fatal("duplicate usage")
					}
					usage = chunk.Usage
				}
				if chunk.Type == provider.ChunkError {
					terminal = chunk.Err
				}
			}
			if usage == nil || !usage.Estimated || usage.PromptTokens != 1050 || usage.CompletionTokens != 2 || usage.CacheWriteBilledTokens != 25 {
				t.Fatalf("usage=%+v", usage)
			}
			if !cancelFirst && !errors.Is(terminal, cause) {
				t.Fatalf("lost read cause: %v", terminal)
			}
		})
	}
}

func TestInterruptedUsageAbandonedConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	out := make(chan provider.Chunk)
	done := make(chan struct{})
	go func() {
		(&client{name: "fixture"}).readStream(ctx, &http.Response{Body: io.NopCloser(strings.NewReader(partialUsageStart + partialUsageText))}, out)
		close(done)
	}()
	// Receiving text proves message_start was parsed. Leave the subsequent
	// usage send without a consumer, then cancel its authority.
	if chunk := <-out; chunk.Type != provider.ChunkText {
		t.Fatalf("first chunk=%+v", chunk)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("partial accounting held an abandoned stream open")
	}
}

func TestInterruptedUsageIdleTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, partialUsageStart)
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	p, err := New(provider.Config{Name: "fixture", BaseURL: srv.URL, Model: "fixture", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	p.(*client).idleTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var usage *provider.Usage
	var terminal error
	for chunk := range ch {
		if chunk.Type == provider.ChunkUsage {
			if usage != nil {
				t.Fatal("duplicate usage")
			}
			usage = chunk.Usage
		}
		if chunk.Type == provider.ChunkError {
			terminal = chunk.Err
		}
	}
	if usage == nil || usage.PromptTokens != 1050 || !usage.Estimated || provider.StreamInterruptReason(terminal) != provider.StreamInterruptIdleTimeout {
		t.Fatalf("usage=%+v error=%v", usage, terminal)
	}
}
