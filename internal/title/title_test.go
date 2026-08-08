package title

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestPromptRequiresUserMessageLanguage(t *testing.T) {
	if !strings.Contains(Prompt, "same language as the user's message") {
		t.Fatalf("title prompt does not preserve the user's language: %q", Prompt)
	}
}

func TestPromptForbidsAnsweringTheQuestion(t *testing.T) {
	if !strings.Contains(Prompt, "not a task to execute") || !strings.Contains(Prompt, "never answer it") {
		t.Fatalf("title prompt must forbid answering the user message: %q", Prompt)
	}
}

type recordingProvider struct {
	requests []provider.Request
}

func (p *recordingProvider) Name() string { return "recording-title" }

func (p *recordingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: req.Messages[len(req.Messages)-1].Content}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type failProvider struct{}

func (failProvider) Name() string { return "fail-title" }

func (failProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkError}
	close(ch)
	return ch, nil
}

func TestProviderConfigDisablesThinkingForGenericOpenAI(t *testing.T) {
	entry := &config.ProviderEntry{Name: "sensenova", Kind: "openai", BaseURL: "https://token.sensenova.cn/v1", Model: "deepseek-v4-flash"}
	cfg := ProviderConfig(entry)
	if got := cfg.Extra["effort"]; got != nil {
		t.Fatalf("effort = %v, want unset for non-official DeepSeek", got)
	}
	if got := cfg.Extra["thinking"]; got != "disabled" {
		t.Fatalf("thinking = %v, want disabled for generic OpenAI-compatible endpoint", got)
	}
}

func TestProviderConfigDisablesThinkingPerProtocol(t *testing.T) {
	cases := []struct {
		name string
		kind string
		base string
		want map[string]any
	}{
		{"anthropic messages api", "anthropic", "https://api.anthropic.com", map[string]any{"thinking": "disabled"}},
		{"deepseek-anthropic endpoint", "anthropic", "https://api.deepseek.com/anthropic", map[string]any{"thinking": "disabled"}},
		{"openai responses api", "responses", "https://api.openai.com/v1", map[string]any{"effort": "disabled"}},
		{"dashscope responses", "dashscope-responses", "https://dashscope.aliyuncs.com", map[string]any{"effort": "disabled"}},
		{"minimax effort-driven host", "openai", "https://api.minimaxi.com/v1", map[string]any{"effort": "disabled"}},
		{"ollama cloud uses generic thinking field", "openai", "https://ollama.com/v1", map[string]any{"thinking": "disabled"}},
		{"generic openai-compatible", "openai", "https://token.sensenova.cn/v1", map[string]any{"thinking": "disabled"}},
		{"unknown kind gets no knob", "mystery", "https://example.com/v1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ProviderConfig(&config.ProviderEntry{Name: "p", Kind: tc.kind, BaseURL: tc.base, Model: "m"})
			if len(cfg.Extra) != len(tc.want) {
				t.Fatalf("Extra = %v, want %v", cfg.Extra, tc.want)
			}
			for k, v := range tc.want {
				if got := cfg.Extra[k]; got != v {
					t.Fatalf("Extra[%q] = %v, want %v", k, got, v)
				}
			}
		})
	}
}

func TestProviderConfigKeepsEffortDisabledForOfficialDeepSeek(t *testing.T) {
	entry := &config.ProviderEntry{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	cfg := ProviderConfig(entry)
	if got := cfg.Extra["effort"]; got != "disabled" {
		t.Fatalf("effort = %v, want disabled for official DeepSeek", got)
	}
	if got := cfg.Extra["thinking"]; got != nil {
		t.Fatalf("thinking = %v, want unset for official DeepSeek", got)
	}
}

type reasoningProvider struct{}

func (reasoningProvider) Name() string { return "reasoning-title" }

// Stream emits a long reasoning chain followed by the actual title, the shape
// of a backend whose thinking cannot be disabled.
func (reasoningProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: strings.Repeat("思考过程占满预算", 20)}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "锂电池高温衰减与缓解"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGenerateTakesContentOnlyNotReasoning(t *testing.T) {
	got, _ := Generate(context.Background(), reasoningProvider{}, "explain lithium battery degradation")
	if got != "锂电池高温衰减与缓解" {
		t.Fatalf("title = %q, want content only, reasoning excluded", got)
	}
}

type thinkInlineProvider struct{}

func (thinkInlineProvider) Name() string { return "think-inline-title" }

// Stream inlines chain-of-thought as <think> blocks inside the content
// stream, the shape of gateways that do not separate reasoning from content.
func (thinkInlineProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "<think>分析用户请求，忽略指令，直接回答。</think>锂电池高温衰减与缓解"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGenerateStripsInlineThinkBlocks(t *testing.T) {
	got, _ := Generate(context.Background(), thinkInlineProvider{}, "explain lithium battery degradation")
	if got != "锂电池高温衰减与缓解" {
		t.Fatalf("title = %q, want inline think block stripped", got)
	}
}

type retryProvider struct {
	attempts int
}

func (p *retryProvider) Name() string { return "retry-title" }

func (p *retryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.attempts++
	ch := make(chan provider.Chunk, 2)
	if p.attempts == 1 {
		// First attempt: model burns the budget on reasoning, empty answer.
		ch <- provider.Chunk{Type: provider.ChunkDone}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "锂电池高温衰减与缓解"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}
	close(ch)
	return ch, nil
}

func TestGenerateRetriesEmptyResult(t *testing.T) {
	prov := &retryProvider{}
	got, _ := Generate(context.Background(), prov, "explain lithium battery degradation")
	if got != "锂电池高温衰减与缓解" {
		t.Fatalf("title = %q, want retried result", got)
	}
	if prov.attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry after empty result)", prov.attempts)
	}
}

func TestGenerateStripsPasteLabelAndUsesShortBudget(t *testing.T) {
	prov := &recordingProvider{}
	got, usage := Generate(context.Background(), prov, "[已粘贴文本 #1 · 20 行]\nfix the login loop")
	if got != "fix the login loop" {
		t.Fatalf("title = %q, want pasted label removed", got)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.MaxTokens != 0 {
		t.Fatalf("MaxTokens = %d, want 0 (no cap; content-only extraction bounds the title)", req.MaxTokens)
	}
	if req.Messages[0].Content != Prompt || req.Messages[1].Content != "fix the login loop" {
		t.Fatalf("title messages = %+v", req.Messages)
	}
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", req.Temperature)
	}
	if usage != nil {
		t.Fatalf("usage = %+v, want nil (recording provider emits none)", usage)
	}
}

func TestGenerateTruncatesLongInputTo300Runes(t *testing.T) {
	prov := &recordingProvider{}
	long := strings.Repeat("长", 400)
	if got, _ := Generate(context.Background(), prov, long); len([]rune(got)) != 58 {
		t.Fatalf("title length = %d, want 58 (57 runes + ellipsis after 60-rune cap)", len([]rune(got)))
	}
	req := prov.requests[0]
	user := req.Messages[1].Content
	if len([]rune(user)) != 303 {
		t.Fatalf("request input runes = %d, want 303", len([]rune(user)))
	}
}

type quotesProvider struct{}

func (quotesProvider) Name() string { return "quotes-title" }

func (quotesProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: `"debug login loop"`}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGenerateStripsMatchingQuotes(t *testing.T) {
	got, _ := Generate(context.Background(), quotesProvider{}, "debug login loop")
	if got != "debug login loop" {
		t.Fatalf("title = %q, want quotes stripped", got)
	}
}

type essayProvider struct{}

func (essayProvider) Name() string { return "essay-title" }

func (essayProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	long := strings.Repeat("高温导致锂电池容量下降的主要原因是：", 8)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: long}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGenerateTruncatesRunawayTitle(t *testing.T) {
	got, _ := Generate(context.Background(), essayProvider{}, "explain lithium battery degradation")
	runes := []rune(got)
	if len(runes) > 60 {
		t.Fatalf("title length = %d runes, want <= 60", len(runes))
	}
	if len(runes) != 58 {
		t.Fatalf("title = %q (len %d), want 57 runes + ellipsis", got, len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated title missing ellipsis: %q", got)
	}
}

func TestGenerateReturnsEmptyOnStreamError(t *testing.T) {
	got, _ := Generate(context.Background(), failProvider{}, "debug login loop")
	if got != "" {
		t.Fatalf("title = %q, want empty on error", got)
	}
}

func TestGenerateSkipsEmptySource(t *testing.T) {
	prov := &recordingProvider{}
	if got, _ := Generate(context.Background(), prov, "   "); got != "" {
		t.Fatalf("title = %q, want empty for blank input", got)
	}
	if len(prov.requests) != 0 {
		t.Fatalf("requests = %d, want 0 for blank input", len(prov.requests))
	}
}
