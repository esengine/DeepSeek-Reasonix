package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"reasonix/internal/provider"
)

type fakeProvider struct {
	stream func(context.Context, provider.Request) (<-chan provider.Chunk, error)
}

func (fakeProvider) Name() string { return "search" }
func (p fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return p.stream(ctx, req)
}

func chunks(values ...provider.Chunk) <-chan provider.Chunk {
	ch := make(chan provider.Chunk, len(values))
	for _, value := range values {
		ch <- value
	}
	close(ch)
	return ch
}

func TestSearchIsolatedRequestsAndBoundedResults(t *testing.T) {
	var mu sync.Mutex
	var requests []provider.Request
	var usages []*provider.Usage
	tool := &Tool{Factory: func() (provider.Provider, error) {
		return fakeProvider{stream: func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
			return chunks(
				provider.Chunk{Type: provider.ChunkReasoning, Text: "PRIVATE REASONING"},
				provider.Chunk{Type: provider.ChunkResponsesItem, ResponsesItem: json.RawMessage(`{"secret":"OPAQUE REPLAY"}`)},
				provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: "one", Raw: json.RawMessage(`[]`), Results: []provider.ServerSearchHit{{Title: "Source", URL: "https://example.com"}, {Title: "Duplicate", URL: "https://example.com"}, {URL: "javascript:alert(1)"}}}},
				provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat("搜索", 4000)},
				provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 20}},
				provider.Chunk{Type: provider.ChunkDone},
			), nil
		}}, nil
	}, ReportUsage: func(u *provider.Usage) { mu.Lock(); usages = append(usages, u); mu.Unlock() }}
	var wg sync.WaitGroup
	for _, q := range []string{"first", "second"} {
		wg.Go(func() {
			output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"`+q+`"}`))
			if err != nil {
				t.Error(err)
				return
			}
			var result Result
			if json.Unmarshal([]byte(output), &result) != nil || len(result.Sources) != 1 || !utf8.ValidString(result.Summary) || len(result.Summary) > maxSummaryBytes {
				t.Errorf("bad result: %s", output)
			}
			if strings.Contains(output, "PRIVATE") || strings.Contains(output, "OPAQUE") {
				t.Error("reasoning or replay escaped search")
			}
		})
	}
	wg.Wait()
	if len(requests) != 2 || len(usages) != 2 {
		t.Fatalf("requests=%d usage=%d", len(requests), len(usages))
	}
	for _, req := range requests {
		if len(req.Messages) != 1 || req.Messages[0].Role != provider.RoleUser || len(req.Tools) != 0 || req.MaxTokens != maxOutputTokens {
			t.Fatalf("unexpected request: %+v", req)
		}
	}
}

func TestSearchRejectsIncompleteOrInventedResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []provider.Chunk
	}{
		{"native error", []provider.Chunk{{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{Raw: json.RawMessage(`{"type":"web_search_tool_result_error","error_code":"unavailable"}`)}}, {Type: provider.ChunkDone}}},
		{"plain prose", []provider.Chunk{{Type: provider.ChunkText, Text: "I searched"}, {Type: provider.ChunkDone}}},
		{"interrupted", []provider.Chunk{{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{Raw: json.RawMessage(`[]`)}}}},
		{"start only", []provider.Chunk{{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: "start"}}, {Type: provider.ChunkDone}}},
		{"client tool", []provider.Chunk{{Type: provider.ChunkToolCall}}},
		{"provider failure", []provider.Chunk{{Type: provider.ChunkError, Err: errors.New("upstream failed")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := &Tool{Factory: func() (provider.Provider, error) {
				return fakeProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
					return chunks(tc.values...), nil
				}}, nil
			}}
			if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"test"}`)); err == nil {
				t.Fatal("expected search failure")
			}
		})
	}
}

func TestSearchCancellationAndValidation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	tool := &Tool{Factory: func() (provider.Provider, error) {
		return fakeProvider{stream: func(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
			close(started)
			ch := make(chan provider.Chunk)
			go func() { <-ctx.Done(); close(ch); close(cancelled) }()
			return ch, nil
		}}, nil
	}}
	for _, args := range []string{`{`, `{}`, `{"query":" "}`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := tool.Execute(ctx, json.RawMessage(`{"query":"test"}`)); finished <- err }()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	<-cancelled
}

func TestSearchEncodedResultBound(t *testing.T) {
	result := Result{Summary: strings.Repeat("\x00", maxSummaryBytes)}
	for range maxSources {
		result.Sources = append(result.Sources, provider.ServerSearchHit{Title: strings.Repeat("\x00", maxSourceBytes), URL: "https://example.com/" + strings.Repeat("a", 1900)})
	}
	output, err := encodeResult(result)
	if err != nil || len(output) > 24000 || !json.Valid([]byte(output)) {
		t.Fatalf("invalid bounded output: size=%d err=%v", len(output), err)
	}
	var got Result
	if err := json.Unmarshal([]byte(output), &got); err != nil || !got.Truncated || len(got.Sources) == 0 {
		t.Fatalf("lost sources or truncation marker: %+v", got)
	}
}

func TestSearchCompletedWithoutStructuredSources(t *testing.T) {
	calls := 0
	status := ""
	tool := &Tool{ReportSourcesStatus: func(value string) { status = value }, Factory: func() (provider.Provider, error) {
		return fakeProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
			calls++
			return chunks(provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: "s", Raw: json.RawMessage(`{"type":"web_search_call","status":"completed"}`)}}, provider.Chunk{Type: provider.ChunkText, Text: "Summary mentioning https://unverified.invalid is still only prose."}, provider.Chunk{Type: provider.ChunkDone}), nil
		}}, nil
	}}
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || status != provider.SourcesNotProvided || result.SourcesStatus != status || len(result.Sources) != 0 || !strings.Contains(result.Summary, "Summary") {
		t.Fatalf("calls=%d result=%+v status=%s", calls, result, status)
	}
}
