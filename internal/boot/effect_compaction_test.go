package boot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const deepSeekOverflowBody = `{"error":{"message":"This model's maximum context length is %d tokens. However, you requested %d tokens (%d in the messages, %d in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`

// denseSummaryProvider drives a read loop and counts summary requests at three
// characters per token, denser than the estimator's cold four, while sampling
// requests count at four. That is the #9818 shape: the ordinary turn fits, the
// summary of the same history does not, and the rejection is DeepSeek's 400.
type denseSummaryProvider struct {
	mu        sync.Mutex
	window    int
	rounds    int
	maxRounds int
	summaries []int // dense token count of every summary request, in order
	samplings []int // wire characters of every sampling request, in order
	overflows int
}

func (p *denseSummaryProvider) Name() string { return "boot-dense-summary" }

func (p *denseSummaryProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{
		WindowMode: provider.ContextWindowShared, AutoOutputTokens: 8192, MaxOutputTokens: 8192,
		LimitMode: provider.OutputLimitOmitWhenSafe,
	}
}

func requestChars(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content) + len(m.ReasoningContent)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments)
		}
	}
	for _, schema := range req.Tools {
		n += len(schema.Name) + len(schema.Description) + len(schema.Parameters)
	}
	return n
}

func isCompactionRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[len(req.Messages)-1].Content, "Compact the preceding conversation prefix")
}

func (p *denseSummaryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	chars := requestChars(req)
	if isCompactionRequest(req) {
		prompt := chars / 3
		p.summaries = append(p.summaries, prompt)
		completion := req.MaxTokens
		if completion <= 0 {
			completion = 8192
		}
		if prompt+completion > p.window {
			p.overflows++
			body := fmt.Sprintf(deepSeekOverflowBody, p.window, prompt+completion, prompt, completion)
			limit := provider.ParseContextLimitError(&provider.APIError{Provider: p.Name(), Status: 400, Body: body})
			if limit == nil {
				return nil, fmt.Errorf("DeepSeek overflow body did not parse: %s", body)
			}
			return nil, limit
		}
		return streamChunks(
			provider.Chunk{Type: provider.ChunkText, Text: "- goal: read big.txt repeatedly\n- pending: keep reading"},
			provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: prompt, CompletionTokens: 12, TotalTokens: prompt + 12, RequestCount: 1}},
			provider.Chunk{Type: provider.ChunkDone},
		), nil
	}
	p.samplings = append(p.samplings, chars)
	prompt := chars / 4
	usage := &provider.Usage{PromptTokens: prompt, CompletionTokens: 10, TotalTokens: prompt + 10, RequestCount: 1}
	if p.rounds >= p.maxRounds {
		return streamChunks(
			provider.Chunk{Type: provider.ChunkText, Text: "Done reading."},
			provider.Chunk{Type: provider.ChunkUsage, Usage: usage},
			provider.Chunk{Type: provider.ChunkDone},
		), nil
	}
	p.rounds++
	return streamChunks(
		provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("read-%d", p.rounds), Name: "read_file", Arguments: fmt.Sprintf(`{"path":"file-%d.txt"}`, p.rounds),
		}},
		provider.Chunk{Type: provider.ChunkUsage, Usage: usage},
		provider.Chunk{Type: provider.ChunkDone},
	), nil
}

func streamChunks(items ...provider.Chunk) <-chan provider.Chunk {
	ch := make(chan provider.Chunk, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

// TestEffectSummaryOverflowShrinksNextSummaryThroughRealBuild pins the
// overflow feedback at its final boundary: when the provider rejects the
// summary request itself, the next summary request that reaches the provider
// is strictly smaller and the tool loop completes instead of dead-ending.
func TestEffectSummaryOverflowShrinksNextSummaryThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	// Results stay under the prune threshold, unique per file, and complete, so
	// neither pruning, duplicate-result folding, nor the incomplete-read strategy
	// can relieve pressure: only a summary can.
	rec := &denseSummaryProvider{window: 40_000, maxRounds: 30}
	provider.Register("boot-dense-summary", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	for i := 1; i <= rec.maxRounds; i++ {
		var body strings.Builder
		for line := range 120 {
			fmt.Fprintf(&body, "file %d line %d: the quick brown fox jumps over the lazy dog\n", i, line)
		}
		writeFile(t, dir, fmt.Sprintf("file-%d.txt", i), body.String())
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-dense-summary"
model = "x"
context_window = 40000
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := ctrl.Run(runCtx, "read every file-N.txt until you are told to stop"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.rounds < rec.maxRounds {
		t.Fatalf("tool loop stopped after %d of %d rounds; the run dead-ended", rec.rounds, rec.maxRounds)
	}
	if rec.overflows == 0 {
		t.Fatalf("no summary request overflowed; the fixture did not reproduce the dense-summary shape (summaries=%v samplings=%v)", rec.summaries, rec.samplings)
	}
	for i := 1; i < len(rec.summaries); i++ {
		if rec.summaries[i-1]+8192 > rec.window && rec.summaries[i] >= rec.summaries[i-1] {
			t.Fatalf("summary request after an overflow did not shrink: %v", rec.summaries)
		}
	}
}
