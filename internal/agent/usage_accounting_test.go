package agent

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type accountingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f accountingRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failedRequestProvider struct{}

func (failedRequestProvider) Name() string { return "failed-request" }

func (failedRequestProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	requestCtx := provider.WithRequestAttemptCounter(ctx)
	client := &http.Client{Transport: accountingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("bad request")),
		}, nil
	})}
	_, err := provider.SendWithRetry(requestCtx, client, provider.SendOptions{Provider: "failed-request"}, func(reqCtx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(reqCtx, http.MethodPost, "https://example.invalid", nil)
	})
	return nil, err
}

func TestMergeStreamUsageCountsProviderRequests(t *testing.T) {
	first := &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	retry := &provider.Usage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28}
	got := mergeStreamUsage(first, retry)
	if got == nil || got.TotalTokens != 43 || got.RequestCount != 2 {
		t.Fatalf("merged usage = %+v, want total=43 requests=2", got)
	}

	third := &provider.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}
	got = mergeStreamUsage(got, third)
	if got.RequestCount != 3 {
		t.Fatalf("nested merged request count = %d, want 3", got.RequestCount)
	}

	got = mergeStreamUsage(nil, retry)
	if got == nil || got.TotalTokens != retry.TotalTokens || got.RequestCount != 2 {
		t.Fatalf("missing first usage = %+v, want retry tokens and 2 requests", got)
	}
	got = mergeStreamUsage(first, nil)
	if got == nil || got.TotalTokens != first.TotalTokens || got.RequestCount != 2 {
		t.Fatalf("missing retry usage = %+v, want first tokens and 2 requests", got)
	}

	requestOnly := &provider.Usage{RequestCount: 3}
	got = mergeStreamUsage(first, requestOnly)
	if got == nil || got.TotalTokens != first.TotalTokens || got.RequestCount != 4 {
		t.Fatalf("request-only retry usage = %+v, want first tokens and 4 requests", got)
	}
}

func TestStreamReturnsRequestOnlyUsageOnProviderFailure(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(failedRequestProvider{}, tool.NewRegistry(), NewSession(""), Options{ModelRef: "failed/model"}, sink)

	_, _, _, _, _, _, _, usage, _, _, _, err := a.stream(context.Background(), 1, sink)
	if err == nil {
		t.Fatal("expected provider failure")
	}
	if usage == nil || usage.TotalTokens != 0 || usage.RequestCount != 1 {
		t.Fatalf("failed stream usage = %+v, want tokens=0 requests=1", usage)
	}
	a.emitTurnUsage(usage, nil)
	if len(events) != 1 || events[0].Kind != event.Usage || events[0].Usage.RequestCount != 1 {
		t.Fatalf("request-only usage event = %+v", events)
	}
}

func TestTaskUsageModelRefUsesCanonicalRuntimeIdentity(t *testing.T) {
	task := (&TaskTool{baseModel: "deepseek/deepseek-v4-pro"}).WithTranscriptIdentityResolver(
		func(modelRef, effort string) (string, string) {
			if modelRef == "flash" {
				return "deepseek/deepseek-v4-flash", effort
			}
			return "deepseek/deepseek-v4-pro", effort
		},
	)
	if got := task.usageModelRef("flash", "high"); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("alias usage model = %q", got)
	}
	if got := task.usageModelRef("", ""); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("inherited usage model = %q", got)
	}
}
