package capability

import (
	"errors"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestSemanticRootUsage(t *testing.T) {
	for _, mode := range []string{"reported", "estimated", "nil_sink", "billed_error", "invalid_answer", "missing", "cache", "no_observer"} {
		t.Run(mode, func(t *testing.T) {
			u := &provider.Usage{PromptTokens: 1000, CompletionTokens: 100}
			u.Estimated = mode == "estimated"
			chunks := []provider.Chunk{{Type: provider.ChunkText, Text: `["skill:fixture"]`}}
			if mode == "invalid_answer" {
				chunks[0].Text = "invalid JSON"
			}
			if mode != "missing" {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkUsage, Usage: u})
			}
			if mode == "billed_error" {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkError, Err: errors.New("fixture failure")})
			}
			sink := &captureSink{}
			r := &SemanticRouter{Provider: &fakeStreamProvider{chunks: chunks}, Sink: sink, Model: "fixture"}
			if mode == "nil_sink" {
				r.Sink = nil
			}
			ctx := t.Context()
			calls := 0
			if mode != "no_observer" {
				ctx = provider.WithAuxiliaryUsageObserver(ctx, func(usage *provider.Usage) {
					calls++
					if usage.PromptTokens != 1000 || usage.CompletionTokens != 100 {
						t.Errorf("observed usage=%+v", usage)
					}
					usage.PromptTokens = 9999
				})
			}
			catalog := Catalog{Entries: []Entry{{ID: "skill:fixture", Kind: KindSkill, Name: "fixture", Description: "fixture skill", Status: StatusReady}}}
			decision := r.RouteSemantic(ctx, "task", catalog, RouteDecision{})
			if mode == "cache" {
				r.RouteSemantic(ctx, "task", catalog, RouteDecision{})
			}
			want := 1
			if mode == "missing" || mode == "no_observer" {
				want = 0
			}
			if calls != want {
				t.Fatalf("observer calls=%d want%d", calls, want)
			}
			if mode != "invalid_answer" && mode != "billed_error" && len(decision.Candidates) != 1 {
				t.Fatalf("route=%+v", decision)
			}
			if mode == "invalid_answer" || mode == "billed_error" {
				if len(decision.Candidates) != 0 {
					t.Fatal("failed semantic answer changed deterministic decision")
				}
			}
			if u.PromptTokens != 1000 {
				t.Fatal("observer changed provider record")
			}
			for _, e := range sink.events {
				if e.Usage != nil && e.Usage.PromptTokens != 1000 {
					t.Fatal("observer changed public usage event")
				}
			}
		})
	}
}
