package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
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
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want retry after reasoning-only answer", prov.call)
	}
	if got := lastAssistantContent(a.sess.conversation); got != "visible reply" {
		t.Fatalf("last assistant content = %q, want visible reply", got)
	}
	if !sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("missing synthetic visible-answer retry message")
	}
}

func TestRunPrefixesReasoningLanguageOnSyntheticRetry(t *testing.T) {
	prov := &mockProvider{name: "p", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I should answer the user."},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible reply"},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{ReasoningLanguage: "zh"}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(prov.requests))
	}
	for i, req := range prov.requests {
		got := lastUser(req)
		if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") {
			t.Fatalf("request %d last user = %q, want reasoning-language prefix", i, got)
		}
	}
	if !strings.Contains(lastUser(prov.requests[1]), "visible answer") {
		t.Fatalf("retry request last user = %q, want visible-answer retry", lastUser(prov.requests[1]))
	}
}

func TestRunStopsAfterRepeatedEmptyFinalAnswers(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking 1"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 2"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 3"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

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

func lastAssistantContent(s *sessionstore.Session) string {
	var out string
	for _, m := range s.Messages {
		if m.Role == provider.RoleAssistant {
			out = m.Content
		}
	}
	return out
}

// deepseekThinkingProvider marks a scripted provider as DeepSeek thinking mode
// (provider.ToolCallReasoningPolicy) — the scope within which a reasoning-only
// finish_reason="stop" turn is accepted as a final answer.
type deepseekThinkingProvider struct{ *scriptedProvider }

func (deepseekThinkingProvider) RequiresToolCallReasoning() bool { return true }

func TestRunAcceptsReasoningOnlyFinalWhenModelStopped(t *testing.T) {
	// DeepSeek thinking mode streams a long reasoning_content and then
	// finishes with finish_reason="stop" but an empty content block. The
	// model has explicitly signalled completion and its reasoning was
	// streamed to the user, so the host must accept the turn instead of
	// retrying and forcing another expensive thinking round.
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "The user asked a simple question; I have reasoned through it and the answer is ready."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (model signalled stop; no retry)", prov.call)
	}
	if sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("must not inject a synthetic visible-answer retry when the model signalled stop")
	}
	if got := lastAssistantContent(a.sess.conversation); got != "" {
		t.Fatalf("last assistant content = %q, want empty (answer lived in reasoning)", got)
	}
}

func TestRunRetriesReasoningOnlyStopWithoutDeepSeekPolicy(t *testing.T) {
	// Same chunk sequence as the accept test, but the provider does not
	// declare DeepSeek thinking mode (ToolCallReasoningPolicy). The accept
	// path must stay scoped to DeepSeek: local <think>-tag models keep the
	// retry safety net that often recovers a visible answer on the second
	// attempt, and a gateway that mislabels truncation as "stop" must not
	// have a degenerate turn committed as the final answer.
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "thinking only, nothing visible"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible reply"},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (non-DeepSeek providers must keep the retry)", prov.call)
	}
	if !sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("missing synthetic visible-answer retry message for non-DeepSeek provider")
	}
	if got := lastAssistantContent(a.sess.conversation); got != "visible reply" {
		t.Fatalf("last assistant content = %q, want visible reply", got)
	}
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
			for range b.N {
				got = hasVisibleFinalAnswer(tc.text)
			}
			_ = got
		})
	}
}

func reasoningOnlyStop(text string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: text},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
		{Type: provider.ChunkDone},
	}
}

func readFileToolRound() []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: "read the file"},
		toolCallChunk("call-1", "read_file", `{}`),
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls", TotalTokens: 10}},
		{Type: provider.ChunkDone},
	}
}

func countUserMessagesContaining(s *sessionstore.Session, needle string) int {
	n := 0
	for _, message := range s.Messages {
		if message.Role == provider.RoleUser && strings.Contains(message.Content, needle) {
			n++
		}
	}
	return n
}

func TestRunRetriesReasoningOnlyStopAfterToolRoundOnce(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		readFileToolRound(),
		reasoningOnlyStop("I have the file contents; the answer is ready."),
		{{Type: provider.ChunkText, Text: "The file says contents."}, {Type: provider.ChunkDone}},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	a := New(deepseekThinkingProvider{prov}, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "what does the file say"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want tool round + reasoning-only stop + one retry", prov.call)
	}
	if got := countUserMessagesContaining(a.sess.conversation, "visible answer"); got != 1 {
		t.Fatalf("visible-answer retries = %d, want exactly 1", got)
	}
	if got := lastAssistantContent(a.sess.conversation); got != "The file says contents." {
		t.Fatalf("last assistant content = %q, want the visible synthesis", got)
	}
}

func TestRunAcceptsSecondReasoningOnlyStopAfterToolRound(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		readFileToolRound(),
		reasoningOnlyStop("done thinking"),
		reasoningOnlyStop("still only thinking"),
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	a := New(deepseekThinkingProvider{prov}, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "what does the file say"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want a single bounded retry", prov.call)
	}
	if got := countUserMessagesContaining(a.sess.conversation, "visible answer"); got != 1 {
		t.Fatalf("visible-answer retries = %d, want exactly 1", got)
	}
}
