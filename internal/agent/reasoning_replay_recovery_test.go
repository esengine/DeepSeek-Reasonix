package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type strictNoWarningReasoningProvider struct{ *testutil.MockProvider }

func (p strictNoWarningReasoningProvider) RequiresToolCallReasoning() bool      { return true }
func (p strictNoWarningReasoningProvider) WarnOnMissingToolCallReasoning() bool { return false }

type strictFallbackReasoningProvider struct {
	*testutil.MockProvider
	mu       sync.Mutex
	fallback []bool
}

func (p *strictFallbackReasoningProvider) RequiresToolCallReasoning() bool      { return true }
func (p *strictFallbackReasoningProvider) WarnOnMissingToolCallReasoning() bool { return true }
func (p *strictFallbackReasoningProvider) SupportsMissingReasoningFallback() bool {
	return true
}
func (p *strictFallbackReasoningProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.fallback = append(p.fallback, provider.MissingReasoningFallbackFromContext(ctx))
	p.mu.Unlock()
	return p.MockProvider.Stream(ctx, req)
}
func (p *strictFallbackReasoningProvider) fallbackCalls() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.fallback...)
}

func TestStrictMissingReasoningCircuitSuppressesDuplicateRetryAcrossRuns(t *testing.T) {
	stateDir := t.TempDir()
	call := provider.ToolCall{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}
	seedProvider := strictAssistantReasoningProvider{testutil.NewMock("strict-replay")}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(seedProvider)
	if !newMissingReasoningWarnState(stateDir).claim(fingerprint) {
		t.Fatal("failed to seed a persisted incident from the previous behavior")
	}

	firstProvider := testutil.NewMock("strict-replay",
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
	)
	firstSink := &recordSink{}
	first := New(strictAssistantReasoningProvider{firstProvider}, echoRegistry(), NewSession(""), Options{
		MissingReasoningWarnStateDir: stateDir,
	}, firstSink)
	var replayErr *ReasoningReplayError
	if err := first.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) {
		t.Fatalf("first Run error = %v, want ReasoningReplayError", err)
	}
	if message := replayErr.Error(); !strings.Contains(message, "exhausted its safe automatic recovery") || !strings.Contains(message, "switch provider or protocol") {
		t.Fatalf("ReasoningReplayError = %q, want exhausted recovery and actionable provider guidance", message)
	}
	if got := firstSink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("active circuit retries = %d, want 0", got)
	}
	if got := firstProvider.CallCount(); got != 1 {
		t.Fatalf("active circuit provider calls = %d, want one terminal probe", got)
	}

	secondProvider := testutil.NewMock("strict-replay",
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
	)
	secondSink := &recordSink{}
	second := New(strictAssistantReasoningProvider{secondProvider}, echoRegistry(), NewSession(""), Options{
		MissingReasoningWarnStateDir: stateDir,
	}, secondSink)
	if err := second.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) {
		t.Fatalf("second Run error = %v, want ReasoningReplayError", err)
	}
	if got := secondSink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("second Run retries = %d, want circuit suppression", got)
	}
	requests := secondProvider.Requests()
	if len(requests) != 1 {
		t.Fatalf("strict circuit requests = %d, want one terminal probe", len(requests))
	}
}

func TestStrictMissingReasoningUsesProviderFallbackBeforeToolExecution(t *testing.T) {
	stateDir := t.TempDir()
	call := func(id, text string) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: "echo", Arguments: `{"text":"` + text + `"}`}
	}
	mock := testutil.NewMock("strict-fallback",
		testutil.Turn{ToolCalls: []provider.ToolCall{call("bad-1", "must not run 1")}, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11, RequestCount: 1}},
		testutil.Turn{ToolCalls: []provider.ToolCall{call("bad-2", "must not run 2")}, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11, RequestCount: 1}},
		testutil.Turn{ToolCalls: []provider.ToolCall{call("safe", "hi")}, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11, RequestCount: 1}},
		testutil.Turn{Text: "done", Usage: &provider.Usage{PromptTokens: 12, CompletionTokens: 1, TotalTokens: 13, RequestCount: 1}},
	)
	prov := &strictFallbackReasoningProvider{MockProvider: mock}
	sink := &recordSink{}
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)

	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := prov.fallbackCalls(), []bool{false, false, true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback calls = %v, want %v", got, want)
	}
	requests := mock.Requests()
	if len(requests) != 4 || !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatalf("requests = %d, want exact replay then fallback tool/final", len(requests))
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 || results[0].Tool.ID != "safe" {
		t.Fatalf("tool results = %+v, want only the fallback tool", results)
	}
	for _, message := range agent.Session().Snapshot() {
		for _, toolCall := range message.ToolCalls {
			if toolCall.ID == "bad-1" || toolCall.ID == "bad-2" {
				t.Fatalf("discarded tool call leaked into session history: %+v", toolCall)
			}
		}
	}
	usages := sink.kinds(event.Usage)
	if len(usages) < 2 || usages[0].Usage == nil || usages[0].Usage.RequestCount != 3 || usages[0].Usage.TotalTokens != 33 {
		t.Fatalf("fallback usage = %+v, want first round to bill exactly three requests/33 tokens", usages)
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 2 || retries[0].RetryScope != event.RetryScopeProtocol || retries[1].RetryScope != event.RetryScopeProtocol {
		t.Fatalf("protocol retry events = %+v, want exact retry and fallback", retries)
	}
}

func TestStrictMissingReasoningOpenCircuitStartsNextSessionInFallback(t *testing.T) {
	stateDir := t.TempDir()
	seed := &strictFallbackReasoningProvider{MockProvider: testutil.NewMock("strict-fallback")}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(seed)
	state := newMissingReasoningWarnState(stateDir)
	if !state.claim(fingerprint) || !state.openFallbackAt(fingerprint, time.Now()) {
		t.Fatal("failed to seed active circuit")
	}
	statePath := filepath.Join(stateDir, missingReasoningWarnStateFilename)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	call := provider.ToolCall{ID: "safe", Name: "echo", Arguments: `{"text":"hi"}`}
	mock := testutil.NewMock("strict-fallback",
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
		testutil.Turn{Text: "done"},
	)
	prov := &strictFallbackReasoningProvider{MockProvider: mock}
	sink := &recordSink{}
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)

	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := prov.fallbackCalls(), []bool{true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback calls = %v, want direct circuit reuse %v", got, want)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("exact retries = %d, want 0 after circuit opened", got)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("intentional fallback responses rewrote the missing-reasoning incident")
	}
}

func TestStrictMissingReasoningHalfOpenFailureSkipsExactReplayAndBacksOff(t *testing.T) {
	stateDir := t.TempDir()
	seed := &strictFallbackReasoningProvider{MockProvider: testutil.NewMock("strict-fallback")}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(seed)
	state := newMissingReasoningWarnState(stateDir)
	openedAt := time.Now().Add(-missingReasoningFallbackBackoffs[0] - 2*time.Second)
	if !state.claimAt(fingerprint, openedAt) || !state.openFallbackAt(fingerprint, openedAt.Add(time.Second)) {
		t.Fatal("failed to seed due half-open circuit")
	}
	call := func(id, text string) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: "echo", Arguments: `{"text":"` + text + `"}`}
	}
	mock := testutil.NewMock("strict-fallback",
		testutil.Turn{ToolCalls: []provider.ToolCall{call("probe-bad", "must not run")}},
		testutil.Turn{ToolCalls: []provider.ToolCall{call("fallback-safe", "hi")}},
		testutil.Turn{Text: "done"},
	)
	prov := &strictFallbackReasoningProvider{MockProvider: mock}
	sink := &recordSink{}
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)

	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := prov.fallbackCalls(), []bool{false, true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("half-open calls = %v, want normal probe then direct fallback %v", got, want)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("half-open exact retries = %d, want 0", got)
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 || results[0].Tool.ID != "fallback-safe" {
		t.Fatalf("tool results = %+v, want only fallback-safe", results)
	}
	incidents, err := state.load(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	incident := incidents[fingerprint]
	if incident.FallbackLevel != 2 {
		t.Fatalf("fallback level = %d, want 2 after failed half-open probe", incident.FallbackLevel)
	}
	if got := time.Unix(0, incident.NextProbeAtUnixNano).Sub(time.Unix(0, incident.FallbackAtUnixNano)); got != missingReasoningFallbackBackoffs[1] {
		t.Fatalf("next probe delay = %v, want %v", got, missingReasoningFallbackBackoffs[1])
	}
}

func TestStrictMissingReasoningHalfOpenClosesAfterThreeHealthyToolRounds(t *testing.T) {
	stateDir := t.TempDir()
	seed := &strictFallbackReasoningProvider{MockProvider: testutil.NewMock("strict-fallback")}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(seed)
	state := newMissingReasoningWarnState(stateDir)
	openedAt := time.Now().Add(-missingReasoningFallbackBackoffs[0] - 2*time.Second)
	if !state.claimAt(fingerprint, openedAt) || !state.openFallbackAt(fingerprint, openedAt.Add(time.Second)) {
		t.Fatal("failed to seed due half-open circuit")
	}
	call := func(id string) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: "echo", Arguments: `{"text":"hi"}`}
	}
	mock := testutil.NewMock("strict-fallback",
		testutil.Turn{Reasoning: "healthy one", ToolCalls: []provider.ToolCall{call("h1")}},
		testutil.Turn{Reasoning: "healthy two", ToolCalls: []provider.ToolCall{call("h2")}},
		testutil.Turn{Reasoning: "healthy three", ToolCalls: []provider.ToolCall{call("h3")}},
		testutil.Turn{Text: "done"},
	)
	prov := &strictFallbackReasoningProvider{MockProvider: mock}
	agent := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, event.Discard)

	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := prov.fallbackCalls(), []bool{false, false, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("healthy half-open calls = %v, want normal thinking %v", got, want)
	}
	if state.fallbackActiveAt(fingerprint, time.Now()) {
		t.Fatal("three healthy half-open rounds did not close fallback circuit")
	}
	if got := state.claimRecoveryModeAt(fingerprint, time.Now()).Mode; got != missingReasoningRecoveryNormal {
		t.Fatalf("post-recovery mode = %v, want normal", got)
	}
}

func TestStrictNoWarningReplayDoesNotLeakSpeculativeEvents(t *testing.T) {
	missingTurn := func(id string) testutil.Turn {
		call := provider.ToolCall{ID: id, Name: "echo", Arguments: `{"text":"must not run"}`}
		return testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "speculative"},
			{Type: provider.ChunkToolCallStart, ToolCall: &call},
			{Type: provider.ChunkToolCall, ToolCall: &call},
			{Type: provider.ChunkDone},
		}}
	}
	providerMock := testutil.NewMock("strict-no-warning", missingTurn("c1"), missingTurn("c2"))
	sink := &recordSink{}
	agent := New(strictNoWarningReasoningProvider{providerMock}, echoRegistry(), NewSession(""), Options{}, sink)

	var replayErr *ReasoningReplayError
	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) {
		t.Fatalf("Run error = %v, want ReasoningReplayError", err)
	}
	if got := providerMock.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want malformed turn plus one exact retry", got)
	}
	for _, kind := range []event.Kind{event.ToolDispatch, event.ToolResult, event.Text, event.Message} {
		if got := len(sink.kinds(kind)); got != 0 {
			t.Fatalf("speculative %v events = %d, want 0", kind, got)
		}
	}
}
