package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestCompatibleMissingReasoningDoesNotRegenerate(t *testing.T) {
	mock := testutil.NewMock("compatible", testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "one", Name: "echo", Arguments: `{"text":"hi"}`}}}, testutil.Turn{Text: "done"})
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mock}, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatal(err)
	}
	if mock.CallCount() != 2 || len(sink.kinds(event.ToolResult)) != 1 || len(sink.kinds(event.Retrying)) != 0 {
		t.Fatalf("calls=%d tools=%d retries=%d", mock.CallCount(), len(sink.kinds(event.ToolResult)), len(sink.kinds(event.Retrying)))
	}
}

type transientHeaderProvider struct{ calls int }

func (*transientHeaderProvider) Name() string { return "transient" }
func (p *transientHeaderProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	return nil, &provider.APIError{Status: 503}
}
func TestMainWaitsAfterFiniteRetriesAndCancels(t *testing.T) {
	p := &transientHeaderProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := recoverySleep
	defer func() { recoverySleep = old }()
	recoverySleep = func(ctx context.Context, d time.Duration) bool {
		if d < time.Minute {
			t.Errorf("wait=%s", d)
		}
		cancel()
		return false
	}
	sink := &recordSink{}
	a := New(p, echoRegistry(), NewSession(""), Options{}, sink)
	err := a.Run(withNoClosedLoop(ctx), "go")
	if !errors.Is(err, context.Canceled) || p.calls != 4 {
		t.Fatalf("calls=%d err=%v", p.calls, err)
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 4 || retries[3].Recovery == nil || !retries[3].Recovery.Waiting {
		t.Fatalf("retries=%+v", retries)
	}
}
func TestSubagentStopsAfterFiniteRetries(t *testing.T) {
	p := &transientHeaderProvider{}
	a := New(p, echoRegistry(), NewSession(""), Options{}, event.Discard)
	err := a.Run(withNoClosedLoop(WithSubagentDepth(context.Background(), 1)), "go")
	if err == nil || p.calls != 4 {
		t.Fatalf("calls=%d err=%v", p.calls, err)
	}
}

func TestTruncatedArgumentsNeverExecute(t *testing.T) {
	mock := testutil.NewMock("m", testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "cut", Name: "echo", Arguments: `{"text":"partial"}`}}, Usage: &provider.Usage{FinishReason: "length"}}, testutil.Turn{Text: "recovered"})
	sink := &recordSink{}
	a := New(mock, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatal(err)
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 || results[0].Tool.Output == "echoed: partial" {
		t.Fatalf("results=%+v", results)
	}
	for _, m := range a.Session().Snapshot() {
		if m.ToolCallID == "cut" && m.ToolRunState != provider.ToolRunNotStarted {
			t.Fatalf("state=%s", m.ToolRunState)
		}
	}
}

func TestPartialStreamNeverEntersContinuousWaiting(t *testing.T) {
	turns := make([]testutil.Turn, maxSamplingAttempts)
	for i := range turns {
		turns[i] = testutil.Turn{Text: "partial", ChunkError: provider.StreamInterrupt(errors.New("closed"), provider.StreamInterruptPrematureEOF)}
	}
	mock := testutil.NewMock("m", turns...)
	sink := &recordSink{}
	a := New(mock, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err == nil {
		t.Fatal("partial stream accepted")
	}
	if mock.CallCount() != 4 {
		t.Fatalf("calls=%d", mock.CallCount())
	}
	for _, e := range sink.kinds(event.Retrying) {
		if e.Recovery != nil && e.Recovery.Waiting {
			t.Fatal("partial generation waited indefinitely")
		}
	}
}

func TestKnownSpendStillStopsRecoveryWithUnknownUsage(t *testing.T) {
	var b runBudget
	b.observe(&provider.Usage{PromptTokens: 1000000, Unknown: true}, &provider.Pricing{Input: 2, Currency: "USD"})
	if axis, _ := b.exceeded(TaskBudget{Cost: 1}); axis != "cost" || b.totals().Priced {
		t.Fatalf("axis=%s totals=%+v", axis, b.totals())
	}
}

func TestPiReferenceRetryScenarios(t *testing.T) {
	for _, tc := range []struct {
		name    string
		turns   []testutil.Turn
		calls   int
		success bool
	}{
		{"temporary_then_success", []testutil.Turn{{StreamError: &provider.APIError{Status: 503}}, {StreamError: &provider.APIError{Status: 503}}, {Text: "done"}}, 3, true},
		{"quota", []testutil.Turn{{StreamError: &provider.APIError{Status: 429, Body: "insufficient_quota"}}}, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testutil.NewMock("reference", tc.turns...)
			a := New(p, echoRegistry(), NewSession(""), Options{}, event.Discard)
			err := a.Run(withNoClosedLoop(context.Background()), "go")
			if (err == nil) != tc.success || p.CallCount() != tc.calls {
				t.Fatalf("calls=%d err=%v", p.CallCount(), err)
			}
		})
	}
}

func TestMixedFailuresCannotRenewRecoveryBudget(t *testing.T) {
	missing := testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "missing", Name: "echo", Arguments: `{"text":"unsafe"}`}}}
	p := testutil.NewMock("strict", testutil.Turn{StreamError: &provider.APIError{Status: 503}}, testutil.Turn{StreamError: &provider.APIError{Status: 503}}, missing, missing)
	sink := &recordSink{}
	a := New(strictToolCallReasoningProvider{p}, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err == nil {
		t.Fatal("invalid reasoning accepted")
	}
	if p.CallCount() != 4 || len(sink.kinds(event.ToolResult)) != 0 {
		t.Fatalf("calls=%d tools=%d", p.CallCount(), len(sink.kinds(event.ToolResult)))
	}
}

type canceledCompletionProvider struct{ cancel context.CancelFunc }

func (*canceledCompletionProvider) Name() string { return "late" }
func (p *canceledCompletionProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "late", Name: "echo", Arguments: `{"text":"late"}`}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	p.cancel()
	return ch, nil
}
func TestCanceledCompletionCannotStartTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &recordSink{}
	a := New(&canceledCompletionProvider{cancel}, echoRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(withNoClosedLoop(ctx), "go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(sink.kinds(event.ToolResult)) != 0 {
		t.Fatal("late completion executed a tool")
	}
}

func TestUnknownToolsAndPlannerNeverEnterContinuousWait(t *testing.T) {
	a := New(&transientHeaderProvider{}, echoRegistry(), NewSession(""), Options{}, event.Discard)
	failure := provider.ClassifyRecovery(&provider.APIError{Status: 503})
	a.turn.writeRecovery = map[string]provider.ToolCall{"unknown": {ID: "unknown"}}
	if a.canWaitSampling(context.Background(), &samplingRecoveryState{}, failure) {
		t.Fatal("unknown tool allowed endless waiting")
	}
	a.turn.writeRecovery = nil
	if a.canWaitSampling(context.Background(), &samplingRecoveryState{}, provider.ClassifyRecovery(&provider.APIError{Status: 409})) {
		t.Fatal("conflict allowed endless waiting")
	}
	ctx := context.WithValue(context.Background(), turnContextRoleKey{}, turnContextPlanner)
	if a.canWaitSampling(ctx, &samplingRecoveryState{}, failure) {
		t.Fatal("planner allowed endless waiting")
	}
}
