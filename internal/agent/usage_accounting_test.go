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

func TestEmitRecoveryAttemptUsageAccountsBillingOnly(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(failedRequestProvider{}, tool.NewRegistry(), NewSession(""), Options{ModelRef: "recovery/model"}, sink)

	// An attempt with real tokens is billed as its own event and never touches
	// lastUsage: the context gauge and compaction decision read the adopted
	// attempt's clean numbers, not a billing sum (#7620).
	a.emitRecoveryAttemptUsage(&provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, CacheMissTokens: 10})
	// A nil attempt (no terminal usage chunk) still records the provider call
	// as a request-only event; stats persist it while frontends ignore it.
	a.emitRecoveryAttemptUsage(nil)
	// An attempt whose stream failed before usage still billed the request.
	a.emitRecoveryAttemptUsage(&provider.Usage{RequestCount: 1})

	if len(events) != 3 {
		t.Fatalf("recovery attempt events = %d, want 3: %+v", len(events), events)
	}
	if events[0].Kind != event.Usage || events[0].Usage == nil ||
		events[0].Usage.TotalTokens != 12 || events[0].Usage.PromptTokens != 10 {
		t.Fatalf("first attempt event = %+v, want the attempt's own tokens", events[0])
	}
	for _, e := range events[1:] {
		if e.Usage == nil || e.Usage.TotalTokens != 0 || e.Usage.RequestCount != 1 {
			t.Fatalf("request-only event = %+v, want total=0 requests=1", e)
		}
	}
	if a.LastUsage() != nil {
		t.Fatalf("recovery accounting updated lastUsage to %+v, want untouched", a.LastUsage())
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
