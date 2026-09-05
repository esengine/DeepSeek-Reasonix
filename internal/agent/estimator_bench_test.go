package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// benchEstimatorSession builds a transcript of roughly `tokens` estimated tokens
// (2000 chars ≈ 500 tokens per assistant turn under the chars/4 fallback).
func benchEstimatorSession(tokens int) *Session {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	for range tokens / 500 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: "continue"},
		)
	}
	return &Session{Messages: msgs}
}

func benchmarkEstimator(b *testing.B, tokens int) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	sess := benchEstimatorSession(tokens)
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: tokens * 4, CompactRatio: 0.5, CompactForceRatio: 0.5,
		RecentKeep: 2, ArchiveDir: b.TempDir(),
	}, event.Discard)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for range b.N {
		sink += a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	}
	if sink == -1 {
		b.Fatal("impossible")
	}
}

func BenchmarkEstimatorCheck200KTokens(b *testing.B) { benchmarkEstimator(b, 200_000) }
func BenchmarkEstimatorCheck1MTokens(b *testing.B)   { benchmarkEstimator(b, 1_000_000) }
