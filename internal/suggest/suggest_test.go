package suggest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// fakeProvider is a minimal provider.Provider that emits a fixed chunk sequence
// on Stream. It lets tests drive NextPrompt deterministically without a network.
type fakeProvider struct {
	name   string
	chunks []provider.Chunk
	err    error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan provider.Chunk, len(f.chunks)+1)
	for _, c := range f.chunks {
		ch <- c
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func textChunk(s string) provider.Chunk { return provider.Chunk{Type: provider.ChunkText, Text: s} }

func TestNextPromptCollectsText(t *testing.T) {
	f := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		textChunk("继续修"),
		textChunk("复那个 bug"),
	}}
	got, err := NextPrompt(context.Background(), f, nil, Options{})
	if err != nil {
		t.Fatalf("NextPrompt error: %v", err)
	}
	if got != "继续修复那个 bug" {
		t.Fatalf("got %q, want %q", got, "继续修复那个 bug")
	}
}

func TestNextPromptTrimmed(t *testing.T) {
	f := &fakeProvider{name: "fake", chunks: []provider.Chunk{
		textChunk("  "),
		textChunk("下一步怎么做"),
		textChunk("  "),
	}}
	got, err := NextPrompt(context.Background(), f, nil, Options{})
	if err != nil {
		t.Fatalf("NextPrompt error: %v", err)
	}
	if got != "下一步怎么做" {
		t.Fatalf("got %q, want %q", got, "下一步怎么做")
	}
}

func TestNextPromptEmptyOutput(t *testing.T) {
	f := &fakeProvider{name: "fake", chunks: []provider.Chunk{textChunk("   ")}}
	got, err := NextPrompt(context.Background(), f, nil, Options{})
	if err != nil {
		t.Fatalf("NextPrompt error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestNextPromptStreamError(t *testing.T) {
	boom := errors.New("stream failed")
	f := &fakeProvider{name: "fake", err: boom}
	_, err := NextPrompt(context.Background(), f, nil, Options{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNextPromptNilProvider(t *testing.T) {
	got, err := NextPrompt(context.Background(), nil, nil, Options{})
	if err != nil || got != "" {
		t.Fatalf("nil provider: got %q, err %v; want empty, nil", got, err)
	}
}

func TestLastTurn(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "旧问题"},
		{Role: provider.RoleAssistant, Content: "旧回答"},
		{Role: provider.RoleUser, Content: "新问题"},
		// Intermediate tool-call turn: must be skipped.
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1"}}},
		{Role: provider.RoleAssistant, Content: "推理中间件", ReasoningContent: "thinking..."},
		{Role: provider.RoleTool, Name: "bash", Content: "tool-out"},
		{Role: provider.RoleAssistant, Content: "最终回答"},
	}
	user, assistant := lastTurn(history)
	if user != "新问题" {
		t.Fatalf("user = %q, want %q", user, "新问题")
	}
	if assistant != "最终回答" {
		t.Fatalf("assistant = %q, want %q", assistant, "最终回答")
	}
}

func TestLastTurnEmptyWhenMissing(t *testing.T) {
	// No user message.
	user, assistant := lastTurn([]provider.Message{{Role: provider.RoleAssistant, Content: "hi"}})
	if user != "" || assistant != "hi" {
		t.Fatalf("got user=%q assistant=%q, want user= assistant=hi", user, assistant)
	}
	// No final assistant message.
	user, assistant = lastTurn([]provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if user != "hi" || assistant != "" {
		t.Fatalf("got user=%q assistant=%q, want user=hi assistant=", user, assistant)
	}
}

func TestBuildMessagesLabelsLastTurn(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "旧问题"},
		{Role: provider.RoleAssistant, Content: "旧回答"},
		{Role: provider.RoleUser, Content: "总结当前项目"},
		{Role: provider.RoleAssistant, Content: "这是最终回答", ToolCalls: nil},
	}
	msgs := buildMessages(history)
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2 (system + user)", len(msgs))
	}
	if msgs[0].Role != provider.RoleSystem {
		t.Fatalf("first should be system, got %q", msgs[0].Role)
	}
	body := msgs[1].Content
	if !strings.Contains(body, "[User Message]\n总结当前项目") {
		t.Fatalf("user label missing: %q", body)
	}
	if !strings.Contains(body, "[Assistant Response]\n这是最终回答") {
		t.Fatalf("assistant label missing: %q", body)
	}
	// Old turn must not appear.
	if strings.Contains(body, "旧问题") || strings.Contains(body, "旧回答") {
		t.Fatalf("stale turn leaked into context: %q", body)
	}
}

func TestBuildMessagesLanguageCN(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "总结当前项目"},
		{Role: provider.RoleAssistant, Content: "这是回答"},
	}
	msgs := buildMessages(history)
	if !strings.Contains(msgs[0].Content, "简体中文") {
		t.Fatalf("expected 简体中文 language constraint, got system: %q", msgs[0].Content)
	}
}

func TestBuildMessagesLanguageEN(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "summarize the project"},
		{Role: provider.RoleAssistant, Content: "here is the answer"},
	}
	msgs := buildMessages(history)
	if !strings.Contains(msgs[0].Content, "English") {
		t.Fatalf("expected English language constraint, got system: %q", msgs[0].Content)
	}
}

func TestShouldFilterSuggestion(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"run the tests", false},
		{"提交代码", false},
		{"done", true},
		{"no suggestion", true},
		{"silence", true},
		{"stay silent", true},
		{"(silence — nothing to do)", true},
		{"thanks", true},
		{"looks good", true},
		{"let me fix that", true},
		{"i'll check", true},
		{"continue", false},            // allowed single word
		{"/test", false},               // slash command allowed
		{"yes", false},                 // allowed single word
		{"继续", false},                 // allowed single CJK word
		{"singlewordnotinlist", true},  // unknown single word
		{"this sentence has more than twelve words in it so it should be filtered as too long for a suggestion here", true},
		{"push this commit and run tests", false},
	}
	for _, c := range cases {
		if got := shouldFilterSuggestion(c.in); got != c.want {
			t.Errorf("shouldFilterSuggestion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

