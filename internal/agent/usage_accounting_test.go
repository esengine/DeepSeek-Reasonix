package agent

import (
	"context"
	"errors"
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
	first := &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CacheWriteTokens: 2, CacheWriteBilledTokens: 2.5, RequestCount: 1}
	retry := &provider.Usage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28, CacheWriteTokens: 3, CacheWriteBilledTokens: 6, RequestCount: 1}
	got := mergeStreamUsage(first, retry)
	if got == nil || got.TotalTokens != 43 || got.RequestCount != 2 || got.CompletionTokens != 13 {
		t.Fatalf("merged usage = %+v, want total=43 requests=2 completion=13", got)
	}
	// Billable PromptTokens align with summed cache hit+miss.
	if got.CacheMissTokens != 30 || got.PromptTokens != 30 {
		t.Fatalf("billable input = prompt %d miss %d, want 30/30", got.PromptTokens, got.CacheMissTokens)
	}
	if got.CacheWriteTokens != 5 || got.CacheWriteBilledTokens != 8.5 {
		t.Fatalf("merged cache writes = raw %d billed %v, want 5/8.5", got.CacheWriteTokens, got.CacheWriteBilledTokens)
	}

	third := &provider.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, RequestCount: 1}
	got = mergeStreamUsage(got, third)
	if got.RequestCount != 3 {
		t.Fatalf("nested merged request count = %d, want 3", got.RequestCount)
	}

	got = mergeStreamUsage(nil, retry)
	if got == nil || got.TotalTokens != retry.TotalTokens || got.RequestCount != 1 {
		t.Fatalf("missing first usage = %+v, want retry tokens and 1 request", got)
	}
	got = mergeStreamUsage(first, nil)
	if got == nil || got.TotalTokens != first.TotalTokens || got.RequestCount != 1 {
		t.Fatalf("missing retry usage = %+v, want first tokens and 1 request", got)
	}

	requestOnly := &provider.Usage{RequestCount: 3}
	got = mergeStreamUsage(first, requestOnly)
	if got == nil || got.RequestCount != 4 {
		t.Fatalf("request-only retry usage = %+v, want 4 requests", got)
	}
}

func TestFinalizeSamplingUsageKeepsLatestPromptContext(t *testing.T) {
	billable := &provider.Usage{
		PromptTokens: 90000, CompletionTokens: 30, TotalTokens: 90030,
		CacheMissTokens: 90000, RequestCount: 3,
	}
	latest := &provider.Usage{PromptTokens: 30000, CompletionTokens: 10, TotalTokens: 30010, CacheMissTokens: 30000, RequestCount: 1}
	got := finalizeSamplingUsage(billable, latest)
	if got == nil || got.PromptTokens != 90000 {
		t.Fatalf("prompt tokens = %+v, want billable total 90000", got)
	}
	if got.ContextPromptTokens != 30000 || got.ContextCompletionTokens != 10 {
		t.Fatalf("context shape = prompt %d completion %d, want latest 30000/10", got.ContextPromptTokens, got.ContextCompletionTokens)
	}
	if got.ContextFillTokens() != 30000 {
		t.Fatalf("ContextFillTokens = %d, want 30000", got.ContextFillTokens())
	}
	completionOnly := &provider.Usage{PromptTokens: 500, ContextCompletionTokens: 20}
	if fill := completionOnly.ContextFillTokens(); fill != 500 {
		t.Fatalf("completion-only ContextFillTokens = %d, want prompt fallback 500", fill)
	}
	if got.CompletionTokens != 30 || got.RequestCount != 3 {
		t.Fatalf("billable fields = %+v, want summed completion/requests", got)
	}
	// lastUsage stores the latest attempt wholesale (prompt+completion of that
	// request), never the billable aggregate.
	if latest.PromptTokens != 30000 || latest.CompletionTokens != 10 {
		t.Fatalf("latest attempt shape mutated: %+v", latest)
	}
}

func TestMergeSamplingUsageKeepsBillableTokensAcrossRequestOnlyAttempt(t *testing.T) {
	first := &provider.Usage{
		PromptTokens: 100, CompletionTokens: 0, TotalTokens: 100,
		CacheMissTokens: 100, RequestCount: 1,
	}
	second := &provider.Usage{RequestCount: 1}
	got := mergeSamplingUsage(first, second)
	if got.PromptTokens != 100 || got.TotalTokens != 100 || got.RequestCount != 2 {
		t.Fatalf("merged billable = %+v, want first tokens + 2 requests", got)
	}
	final := finalizeSamplingUsage(got, second)
	if final == nil || final.PromptTokens != 100 {
		t.Fatalf("final usage = %+v, want billable prompt 100", final)
	}
}

func TestEstimateFailedAttemptUsageIncludesArgChars(t *testing.T) {
	frozen := samplingRequest{
		req: provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "write a large file"}}},
	}
	// ~8KB of streamed tool args with no terminal usage.
	result := streamedTurn{
		maxArgChars: 8192,
		err:         &provider.StreamInterruptedError{Err: io.ErrUnexpectedEOF, Reason: provider.StreamInterruptPrematureEOF},
		interrupted: true,
	}
	got := estimateFailedAttemptUsage(nil, frozen, result, 1)
	if got == nil || !got.Estimated {
		t.Fatalf("usage = %+v, want estimated failed-attempt record", got)
	}
	argTokens := (8192 + 3) / 4
	if got.CompletionTokens < argTokens {
		t.Fatalf("completion tokens = %d, want at least arg estimate %d", got.CompletionTokens, argTokens)
	}
	if got.PromptTokens <= 0 {
		t.Fatalf("prompt tokens = %d, want request input estimate", got.PromptTokens)
	}
}

func TestEstimateFailedAttemptUsageSkipsZeroHTTPLocalFailure(t *testing.T) {
	frozen := samplingRequest{
		req: provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}},
	}
	result := streamedTurn{
		err: errors.New("local request validation failed"),
	}
	// No HTTP request and no speculative output: do not invent billable usage.
	got := estimateFailedAttemptUsage(nil, frozen, result, 0)
	if got != nil {
		t.Fatalf("pre-body local reject usage = %+v, want nil (no invented billable tokens)", got)
	}
	first := &provider.Usage{PromptTokens: 100, TotalTokens: 100, CacheMissTokens: 100, RequestCount: 1}
	merged := mergeSamplingUsage(first, got)
	if merged == nil || merged.PromptTokens != 100 || merged.RequestCount != 1 {
		t.Fatalf("merged after local reject = %+v, want first attempt only", merged)
	}
}

func TestStreamReturnsRequestOnlyUsageOnProviderFailure(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(failedRequestProvider{}, tool.NewRegistry(), NewSession(""), Options{ModelRef: "failed/model"}, sink)

	st := a.stream(context.Background(), 1, sink)
	if st.err == nil {
		t.Fatal("expected provider failure")
	}
	if st.usage == nil || st.usage.TotalTokens != 0 || st.usage.RequestCount != 1 {
		t.Fatalf("failed stream usage = %+v, want tokens=0 requests=1", st.usage)
	}
	a.emitTurnUsage(st.usage, nil)
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
