package agent

import (
	"context"
	"errors"
	"fmt"
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

type deepseekThinkingProvider struct{ *scriptedProvider }

func (deepseekThinkingProvider) RequiresToolCallReasoning() bool { return true }

func TestRunRequiresVisibleFinalAfterStop(t *testing.T) {
	for _, deepseek := range []bool{false, true} {
		for _, text := range []string{"", " \n\t", "visible reply"} {
			t.Run(fmt.Sprintf("deepseek=%t/text=%q", deepseek, text), func(t *testing.T) {
				first := append([]provider.Chunk{{Type: provider.ChunkText, Text: text}}, reasoningOnlyStop("The answer is ready.")...)
				prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
					first,
					{{Type: provider.ChunkText, Text: "recovered reply"}, {Type: provider.ChunkDone}},
				}}
				var p provider.Provider = prov
				if deepseek {
					p = deepseekThinkingProvider{prov}
				}
				sink := &recordSink{}
				a := New(p, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, sink)
				if err := a.Run(context.Background(), "answer me"); err != nil {
					t.Fatalf("Run: %v", err)
				}
				wantCalls, wantText, wantNotices := 2, "recovered reply", 1
				if strings.TrimSpace(text) != "" {
					wantCalls, wantText, wantNotices = 1, text, 0
				}
				if prov.call != wantCalls {
					t.Fatalf("provider calls = %d, want %d", prov.call, wantCalls)
				}
				if got := lastAssistantContent(a.sess.conversation); got != wantText {
					t.Fatalf("last assistant content = %q, want %q", got, wantText)
				}
				notices := 0
				for _, e := range sink.kinds(event.Notice) {
					if e.Code == event.NoticeCodeEmptyFinal {
						notices++
					}
				}
				if notices != wantNotices {
					t.Fatalf("empty-answer notices = %d, want %d", notices, wantNotices)
				}
			})
		}
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

func TestRunRejectsRepeatedDeepSeekReasoningOnlyStops(t *testing.T) {
	for _, afterTool := range []bool{false, true} {
		t.Run(fmt.Sprintf("afterTool=%t", afterTool), func(t *testing.T) {
			prov := &scriptedProvider{name: "p"}
			reg := tool.NewRegistry()
			wantCalls := 3
			if afterTool {
				prov.turns = append(prov.turns, readFileToolRound())
				reg.Add(fakeReadFileTool{})
				wantCalls++
			}
			prov.turns = append(prov.turns, reasoningOnlyStop("first"), reasoningOnlyStop("second"), reasoningOnlyStop("third"))
			a := New(deepseekThinkingProvider{prov}, reg, sessionstore.NewSession(""), Options{}, event.Discard)
			err := a.Run(context.Background(), "answer me")
			var empty *EmptyFinalError
			if !errors.As(err, &empty) {
				t.Fatalf("error = %v, want *EmptyFinalError", err)
			}
			if empty.Blocks != 3 || empty.FinishReason != "stop" {
				t.Fatalf("EmptyFinalError = %+v, want 3 blocks ending on stop", empty)
			}
			if prov.call != wantCalls {
				t.Fatalf("provider calls = %d, want %d", prov.call, wantCalls)
			}
		})
	}
}

func lengthCutReasoning(text string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: text},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length", TotalTokens: 10}},
		{Type: provider.ChunkDone},
	}
}

// A response whose reasoning ran into the output limit has no visible text
// because it was cut off, not because the model chose to stay silent. Only the
// host knows which, so the retry has to say it.
func TestEmptyFinalRetryTellsTheModelItsResponseWasCutOff(t *testing.T) {
	prov := &mockProvider{name: "p", streams: [][]provider.Chunk{
		lengthCutReasoning("long thinking"),
		{{Type: provider.ChunkText, Text: "visible reply"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(prov.requests))
	}
	got := lastUser(prov.requests[1])
	if !sessionstore.IsSyntheticUserText(got) || !strings.Contains(got, "output-token limit") || !strings.Contains(got, "visible answer") {
		t.Fatalf("retry message = %q, want the host fact that the response hit the output limit, and the ask for a visible answer", got)
	}
}

func TestEmptyFinalRetryWithoutATruncationKeepsTheGenericAsk(t *testing.T) {
	prov := &mockProvider{name: "p", streams: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "visible reply"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := lastUser(prov.requests[1]); strings.Contains(got, "output-token limit") {
		t.Fatalf("retry message = %q, claims a truncation the host never saw", got)
	}
}

func TestRepeatedEmptyFinalCarriesTheFinishReason(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		lengthCutReasoning("1"), lengthCutReasoning("2"), lengthCutReasoning("3"),
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	err := a.Run(context.Background(), "answer me")
	var empty *EmptyFinalError
	if !errors.As(err, &empty) {
		t.Fatalf("error = %v, want *EmptyFinalError", err)
	}
	if empty.Blocks != 3 || empty.FinishReason != "length" {
		t.Fatalf("EmptyFinalError = %+v, want 3 blocks ending on finish reason length", empty)
	}
	if !strings.Contains(err.Error(), "output-token limit") {
		t.Fatalf("error text = %q, want the cause the host knows", err)
	}
}
