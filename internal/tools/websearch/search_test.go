package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

type fixtureProvider struct {
	chunks  []provider.Chunk
	request provider.Request
}

func (*fixtureProvider) Name() string { return "fixture" }
func (p *fixtureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.request = req
	ch := make(chan provider.Chunk, len(p.chunks))
	for _, c := range p.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestSearchRequiresNativeCompletionAndIsolatesHistory(t *testing.T) {
	for name, chunks := range map[string][]provider.Chunk{
		"text without native search": {{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone}},
		"interrupted search":         {{Type: provider.ChunkProviderTool, ToolCall: &provider.ToolCall{Name: "web_search"}}},
		"client tool requested":      {{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{Name: "bash"}}, {Type: provider.ChunkDone}},
	} {
		t.Run(name, func(t *testing.T) {
			p := &fixtureProvider{chunks: chunks}
			tool := &Tool{Factory: func() (provider.Provider, error) { return p, nil }}
			if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"fixture"}`)); !errors.Is(err, ErrIncomplete) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	p := &fixtureProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: "private provider reasoning"},
		{Type: provider.ChunkResponsesItem, ResponsesItem: json.RawMessage(`{"opaque":"replay"}`)},
		{Type: provider.ChunkProviderTool, ToolCall: &provider.ToolCall{Name: "web_search"}, Text: "https://example.com/fixture"},
		{Type: provider.ChunkText, Text: "neutral summary"},
		{Type: provider.ChunkDone},
	}}
	tool := &Tool{Factory: func() (provider.Provider, error) { return p, nil }}
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary != "neutral summary" || result.Sources != "https://example.com/fixture" {
		t.Fatalf("result = %+v", result)
	}
	if len(p.request.Messages) != 1 || len(p.request.Tools) != 0 || p.request.Messages[0].Role != provider.RoleUser {
		t.Fatalf("search request = %+v", p.request)
	}
}

func TestSearchBoundsEncodedOutput(t *testing.T) {
	p := &fixtureProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: strings.Repeat("\x00", 12000)},
		{Type: provider.ChunkProviderTool, ToolCall: &provider.ToolCall{Name: "web_search"}, Text: "https://example.com"},
		{Type: provider.ChunkDone},
	}}
	tool := &Tool{Factory: func() (provider.Provider, error) { return p, nil }}
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 24000 || !json.Valid([]byte(got)) {
		t.Fatalf("encoded output length = %d", len(got))
	}
}

func TestSearchRejectsInvalidAndCancelledQueries(t *testing.T) {
	tool := &Tool{}
	for _, args := range []string{`{`, `{"query":" "}`, `{"query":"` + strings.Repeat("x", 4097) + `"}`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(args)); !errors.Is(err, ErrQuery) {
			t.Fatalf("query error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, json.RawMessage(`{"query":"fixture"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
