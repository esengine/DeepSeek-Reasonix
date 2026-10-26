package boundedllm

import (
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestBoundedUsageObserverOwnsOneTerminal(t *testing.T) {
	for _, mode := range []string{"reported", "billed_failure", "without_sink", "missing", "input_refused"} {
		t.Run(mode, func(t *testing.T) {
			observed, emitted := 0, 0
			original := provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, RequestCount: 2}
			failure := errors.New("review failure")
			chunks := chunkProvider{{Type: provider.ChunkText, Text: "reviewed"}}
			if mode != "missing" {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkUsage, Usage: &original})
			}
			if mode == "billed_failure" {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkError, Err: failure})
			}
			ctx := provider.WithAuxiliaryUsageObserver(t.Context(), func(u *provider.Usage) {
				observed++
				if u.PromptTokens != 1000 || u.RequestCount != 2 {
					t.Errorf("observer usage=%+v", u)
				}
				u.PromptTokens = 9999
			})
			cfg := Config{Provider: chunks, UsageSource: event.UsageSourceAdvisor, Sink: event.FuncSink(func(e event.Event) {
				emitted++
				if e.Usage == nil || e.Usage.PromptTokens != 1000 {
					t.Errorf("observer corrupted emitted usage=%+v", e.Usage)
				}
			})}
			if mode == "without_sink" {
				cfg.Sink = nil
				cfg.UsageSource = ""
			}
			if mode == "input_refused" {
				cfg.MaxSystemBytes = 1
			}
			text, err := Call(ctx, cfg, "policy", "evidence")
			if mode == "billed_failure" {
				if !errors.Is(err, failure) {
					t.Fatalf("error cause lost:%v", err)
				}
			} else if mode == "input_refused" {
				if err == nil {
					t.Fatal("oversized input was admitted")
				}
			} else if err != nil || text != "reviewed" {
				t.Fatalf("answer=%q err=%v", text, err)
			}
			want := 1
			if mode == "missing" || mode == "input_refused" {
				want = 0
			}
			if observed != want || original.PromptTokens != 1000 {
				t.Fatalf("observed=%d want%d original=%+v", observed, want, original)
			}
			wantEmit := want
			if mode == "without_sink" {
				wantEmit = 0
			}
			if emitted != wantEmit {
				t.Fatalf("usage events=%d,want%d", emitted, wantEmit)
			}
		})
	}
}
