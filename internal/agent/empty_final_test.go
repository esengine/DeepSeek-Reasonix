package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func textTurn(text string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: text}, {Type: provider.ChunkDone}}
}

func TestRunAcceptsReasoningOnlyFinalAnswer(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I should answer the user."},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want one clean reasoning-only completion", prov.call)
	}
	if got := lastAssistantContent(a.sess.conversation); got != "" {
		t.Fatalf("last assistant content = %q, want empty content beside reasoning", got)
	}
	if sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("must not inject a synthetic visible-answer retry")
	}
}

func TestRunPrefixesReasoningLanguageOnReasoningOnlyCompletion(t *testing.T) {
	prov := &mockProvider{name: "p", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I should answer the user."},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{ReasoningLanguage: "zh"}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(prov.requests))
	}
	got := lastUser(prov.requests[0])
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") {
		t.Fatalf("last user = %q, want reasoning-language prefix", got)
	}
	if strings.Contains(got, "visible answer") {
		t.Fatalf("reasoning-only completion must not receive a synthetic visible-answer retry: %q", got)
	}
}

func TestRunRequiresVisibleFinalOnlyWhenExplicitlyRequested(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking 1"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 2"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 3"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{RequireVisibleFinal: true}, event.Discard)

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

func TestRunRetriesZeroContentWithTheSameFrozenRequest(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "visible reply"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one empty-response retry", prov.call)
	}
	if len(prov.requests) != 2 || !reflect.DeepEqual(prov.requests[0], prov.requests[1]) {
		t.Fatalf("retry requests differ; want the same frozen request:\nfirst=%#v\nsecond=%#v", prov.requests[0], prov.requests[1])
	}
	if sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("empty-response retry must not inject a synthetic user prompt")
	}
	if got := lastAssistantContent(a.sess.conversation); got != "visible reply" {
		t.Fatalf("last assistant content = %q, want successful retry answer", got)
	}
}

func TestRunStopsAfterExhaustedZeroContentRetriesWithoutCommittingEmptyMessages(t *testing.T) {
	turns := make([][]provider.Chunk, maxSamplingAttempts)
	for i := range turns {
		turns[i] = []provider.Chunk{{Type: provider.ChunkDone}}
	}
	prov := &scriptedProvider{name: "p", turns: turns}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, sink)

	err := a.Run(context.Background(), "answer me")
	if !errors.Is(err, provider.ErrEmptyResponse) {
		t.Fatalf("Run error = %v, want ErrEmptyResponse", err)
	}
	if prov.call != maxSamplingAttempts {
		t.Fatalf("provider calls = %d, want %d bounded attempts", prov.call, maxSamplingAttempts)
	}
	for _, message := range a.sess.conversation.Messages {
		if message.Role == provider.RoleAssistant {
			t.Fatalf("empty attempt committed assistant message: %+v", message)
		}
		if message.Role == provider.RoleUser && strings.Contains(message.Content, "visible answer") {
			t.Fatalf("empty attempt injected synthetic user prompt: %q", message.Content)
		}
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != maxStreamRecoveries {
		t.Fatalf("retry events = %d, want %d", len(retries), maxStreamRecoveries)
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
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

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

func TestRunAcceptsReasoningOnlyStopWithoutDeepSeekPolicy(t *testing.T) {
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
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 for a reasoning-only clean terminal", prov.call)
	}
	if sessionHasUserMessageContaining(a.sess.conversation, "visible answer") {
		t.Fatal("must not inject a synthetic visible-answer retry")
	}
	if got := lastAssistantContent(a.sess.conversation); got != "" {
		t.Fatalf("last assistant content = %q, want empty content beside reasoning", got)
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
