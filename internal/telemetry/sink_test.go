package telemetry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

type readinessSink struct {
	events   int
	audits   int
	recovery int
}

func (s *readinessSink) Emit(event.Event) { s.events++ }
func (s *readinessSink) RecordReadinessAudit(evidence.ReadinessAudit) {
	s.audits++
}
func (s *readinessSink) RecordProtocolRecovery(event.ProtocolRecoveryAudit) {
	s.recovery++
}

func TestSinkWritesOnlyWhitelistedContentFreeCounters(t *testing.T) {
	home := t.TempDir()
	reporter := &Reporter{
		home:    home,
		version: "v1.20.0",
		static: []Counter{
			{Signal: "client_surface", Bucket: "cli", Count: 1},
			{Signal: "cli_mode", Bucket: "run", Count: 1},
		},
	}
	inner := &readinessSink{}
	sink := reporter.Wrap(inner)
	secret := "PRIVATE_PROMPT_TOKEN_123"
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Text, Text: secret})
	sink.Emit(event.Event{Kind: event.Message, Text: secret, Reasoning: secret})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		FinishReason: "stop", CacheHitTokens: 90, CacheMissTokens: 10,
	}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: secret, Args: secret, Output: secret, Err: "permission denied: " + secret,
	}})
	event.RecordProtocolRecovery(sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryReplaced})
	sink.Emit(event.Event{Kind: event.TurnDone, Err: &provider.APIError{
		Provider: secret, Status: 429, Body: secret, TraceID: secret,
	}})
	event.RecordReadinessAudit(sink, evidence.ReadinessAudit{})

	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("pending files = %d, err = %v", len(entries), err)
	}
	b, err := os.ReadFile(filepath.Join(home, pendingDirName, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("pending payload leaked private content: %s", b)
	}
	var payload pendingPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, counter := range payload.Counters {
		got[counter.Signal] = counter.Bucket
	}
	for signal, bucket := range map[string]string{
		"client_surface":               "cli",
		"cli_mode":                     "run",
		"turns":                        "count",
		"finish_reason":                "stop",
		"cache_hit":                    "90_100",
		"tool_error":                   "permission",
		"provider_error":               "rate_limit",
		"cli_exit":                     "error",
		"tool_call_reasoning_recovery": "missing_reasoning_retry_replaced_response",
	} {
		if got[signal] != bucket {
			t.Errorf("%s bucket = %q, want %q", signal, got[signal], bucket)
		}
	}
	if inner.events != 6 || inner.audits != 1 || inner.recovery != 1 {
		t.Fatalf("forwarding events=%d audits=%d recovery=%d", inner.events, inner.audits, inner.recovery)
	}
}

func TestCompletionValidationMetricsAreImmediateAndIsolated(t *testing.T) {
	home := t.TempDir()
	reporter := &Reporter{home: home, version: "v1.34.0"}
	sink := reporter.Wrap(event.Discard)
	secret := "PRIVATE_COMPLETION_TEXT_456"
	sink.Emit(event.Event{Kind: event.Usage, UsageSource: event.UsageSourceCompletionEvaluator, Usage: &provider.Usage{
		FinishReason: "stop", CacheHitTokens: 90, CacheMissTokens: 10,
	}})
	sink.Emit(event.Event{Kind: event.Notice, Text: secret, Reasoning: secret})
	event.RecordCompletionValidation(sink, event.CompletionValidationInfo{
		Mode: "enforce", Outcome: "error", Attempt: 2, DurationMs: 5_200, ErrorClass: "timeout",
	})

	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 2 {
		t.Fatalf("immediate pending files = %d, err = %v", len(entries), err)
	}
	got := map[string]map[string]int{}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(home, pendingDirName, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), secret) {
			t.Fatalf("pending payload leaked completion content: %s", b)
		}
		var payload pendingPayload
		if err := json.Unmarshal(b, &payload); err != nil {
			t.Fatal(err)
		}
		for _, counter := range payload.Counters {
			if got[counter.Signal] == nil {
				got[counter.Signal] = map[string]int{}
			}
			got[counter.Signal][counter.Bucket] += counter.Count
		}
	}
	want := map[string]string{
		"completion_evaluator_finish_reason": "stop",
		"completion_evaluator_cache_hit":     "90_100",
		"completion_validation_outcome":      "enforce_error",
		"completion_validation_latency":      "s_5_15",
		"completion_validation_attempt":      "repair",
		"completion_validation_error":        "timeout",
	}
	for signal, bucket := range want {
		if got[signal][bucket] != 1 {
			t.Errorf("%s/%s = %d, want 1", signal, bucket, got[signal][bucket])
		}
	}
	if got["finish_reason"] != nil || got["cache_hit"] != nil {
		t.Fatalf("evaluator usage polluted generic metrics: %+v", got)
	}
}

func TestCompletionValidationLatencyBucketsStayBounded(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want string
	}{
		{ms: -1, want: "lt_1s"},
		{ms: 999, want: "lt_1s"},
		{ms: 1_000, want: "s_1_5"},
		{ms: 5_000, want: "s_5_15"},
		{ms: 15_000, want: "s_15_60"},
		{ms: 60_000, want: "s_15_60"},
	} {
		if got := completionValidationLatencyBucket(tc.ms); got != tc.want {
			t.Errorf("completionValidationLatencyBucket(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestCleanupRemovesPendingQueueOnly(t *testing.T) {
	home := t.TempDir()
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	idPath := filepath.Join(home, "cli-telemetry-install-id")
	if err := os.WriteFile(idPath, []byte(strings.Repeat("a", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, pendingDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending directory still exists: %v", err)
	}
	if _, err := os.Stat(idPath); err != nil {
		t.Fatalf("install id should remain stable after opt-out cleanup: %v", err)
	}
}

func TestEnvironmentOptOutRemovesPendingQueue(t *testing.T) {
	clearPolicyEnv(t)
	home := t.TempDir()
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DO_NOT_TRACK", "1")
	if reporter := Start(Options{Mode: "on", Version: "v1.20.0", HomeDir: home, Interactive: true}); reporter != nil {
		t.Fatal("environment opt-out unexpectedly started telemetry")
	}
	if _, err := os.Stat(filepath.Join(home, pendingDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("environment opt-out did not remove pending queue: %v", err)
	}
}
