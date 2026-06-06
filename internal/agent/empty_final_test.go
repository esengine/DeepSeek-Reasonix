package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRunRetriesReasoningOnlyFinalAnswer(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I should answer the user."},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible reply"},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want retry after reasoning-only answer", prov.call)
	}
	if got := lastAssistantContent(a.session); got != "visible reply" {
		t.Fatalf("last assistant content = %q, want visible reply", got)
	}
	if !sessionHasUserMessageContaining(a.session, "visible answer") {
		t.Fatal("missing synthetic visible-answer retry message")
	}
}

func TestRunStopsAfterRepeatedEmptyFinalAnswers(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking 1"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 2"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 3"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	err := a.Run(context.Background(), "answer me")
	if err == nil {
		t.Fatal("expected repeated empty final answers to stop the run")
	}
	if !strings.Contains(err.Error(), "visible final answer") {
		t.Fatalf("error = %v, want visible final answer", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want three empty-answer attempts", prov.call)
	}
}

func lastAssistantContent(s *Session) string {
	var out string
	for _, m := range s.Messages {
		if m.Role == provider.RoleAssistant {
			out = m.Content
		}
	}
	return out
}

func BenchmarkHasVisibleFinalAnswer(b *testing.B) {
	cases := []struct {
		name string
		text string
	}{
		{"normal", "visible reply"},
		{"leading-space", strings.Repeat(" ", 256) + "visible reply"},
		{"all-space", strings.Repeat(" \n\t", 256)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var got bool
			for i := 0; i < b.N; i++ {
				got = hasVisibleFinalAnswer(tc.text)
			}
			_ = got
		})
	}
}
