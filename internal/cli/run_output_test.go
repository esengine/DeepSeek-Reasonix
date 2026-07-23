package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestRunOutputTextPrintsOnlyFinalMessage(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputText)
	sink.Emit(event.Event{Kind: event.Text, Text: "streamed "})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Output: "noise"}})
	sink.Emit(event.Event{Kind: event.Message, Text: "final answer"})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "final answer\n" {
		t.Fatalf("text output = %q", got)
	}
}

func TestRunOutputJSONResult(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Text: "done"})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 12, CompletionTokens: 3, CacheHitTokens: 8, CacheMissTokens: 4,
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}
	if result.Type != "result" || result.Subtype != "success" || result.IsError || result.Result != "done" || result.SessionID != "abc" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 || result.Usage.CacheReadInputTokens != 8 || result.Usage.CacheCreationInputTokens != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestRunOutputSessionIDPreservesExistingFormats(t *testing.T) {
	const raw = "20260723-120000.000000000-model"
	identityKey := bytes.Repeat([]byte{0x41}, machineIdentityKeyBytes)
	for _, format := range []runOutputFormat{runOutputText, runOutputJSON, runOutputStreamJSON} {
		if got := runOutputSessionID(format, raw, nil); got != raw {
			t.Fatalf("format %q session id = %q, want raw id %q", format, got, raw)
		}
	}
	if got := runOutputSessionID(runOutputEventsJSONL, raw, identityKey); got != machineSessionIDWithKey(raw, identityKey) {
		t.Fatalf("events-jsonl session id = %q, want machine id %q", got, machineSessionIDWithKey(raw, identityKey))
	}
}

func TestRunOutputStreamJSONEndsWithErrorResult(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputStreamJSON)
	sink.Emit(event.Event{Kind: event.Text, Text: "partial"})
	runErr := errors.New("provider failed")
	if err := sink.Finalize("abc", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stream lines = %d, want 2\n%s", len(lines), out.String())
	}
	var wire map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &wire); err != nil || wire["kind"] != "text" {
		t.Fatalf("wire event = %#v, err=%v", wire, err)
	}
	var result runResult
	if err := json.Unmarshal([]byte(lines[1]), &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Subtype != "error_during_execution" || result.Result != runErr.Error() {
		t.Fatalf("error result = %+v", result)
	}
}

func TestRunOutputEventsJSONLIsStructuredAndRedacted(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputEventsJSONL)
	sink.Emit(event.Event{Kind: event.Text, Text: "PRIVATE ANSWER"})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "PRIVATE TOOL ID", Name: "PRIVATE TOOL NAME", Args: `{"command":"PRIVATE COMMAND"}`, Output: "PRIVATE OUTPUT", Err: "PRIVATE ERROR",
	}})
	sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "PRIVATE TOOL ID", Name: "PRIVATE TOOL NAME"}})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 4, CompletionTokens: 2}})
	if err := sink.Finalize("session-1", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("event lines = %d, output = %s", len(lines), out.String())
	}
	var toolAliases []struct {
		ToolID   string `json:"tool_id"`
		ToolName string `json:"tool_name"`
	}
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if payload["schema_version"] != float64(machineSchemaVersion) || payload["sequence"] != float64(i+1) {
			t.Fatalf("line %d envelope = %#v", i, payload)
		}
		if payload["kind"] == "tool_result" || payload["kind"] == "tool_progress" {
			var aliases struct {
				ToolID   string `json:"tool_id"`
				ToolName string `json:"tool_name"`
			}
			if err := json.Unmarshal([]byte(line), &aliases); err != nil {
				t.Fatal(err)
			}
			toolAliases = append(toolAliases, aliases)
		}
	}
	if strings.Contains(out.String(), "PRIVATE") || !strings.Contains(out.String(), `"kind":"run_done"`) {
		t.Fatalf("event stream was not redacted or terminated: %s", out.String())
	}
	if len(toolAliases) != 2 || toolAliases[0].ToolID != "tool_1" || toolAliases[0].ToolName != "tool_name_1" || toolAliases[1] != toolAliases[0] {
		t.Fatalf("tool aliases = %+v, want stable per-run opaque identities", toolAliases)
	}
}

func TestEventsJSONLHasOneCanonicalFlag(t *testing.T) {
	if _, err := parseRunOutputFormat("events-jsonl"); err == nil {
		t.Fatal("events-jsonl must use the dedicated --events-jsonl flag")
	}
	var code int
	stderr := captureStderr(t, func() {
		code = runAgent([]string{"--events-jsonl", "--output-format", "json", "task"})
	})
	if code != 2 || !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

func TestRunOutputJSONClassifiesRecoveryPauseAsControlledOutcome(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	runErr := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "automatic recovery paused"})
	if err := sink.Finalize("abc", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.Subtype != event.TurnOutcomeRecoveryPaused || result.Result != runErr.Error() || result.NumTurns != 1 {
		t.Fatalf("recovery pause result = %+v", result)
	}
}

func TestRunOutputEventsJSONLClassifiesRecoveryPauseAsControlledOutcome(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputEventsJSONL)
	runErr := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "automatic recovery paused"})
	if err := sink.Finalize("machine-session", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	var result machineRunDone
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.NumTurns != 1 || result.SessionID != "machine-session" {
		t.Fatalf("recovery pause result = %+v", result)
	}
}

func TestClassifyRunCompletion(t *testing.T) {
	pause := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "paused"})
	if got := classifyRunCompletion(pause); got.outcome != event.TurnOutcomeRecoveryPaused || got.isError || got.exitCode != 0 {
		t.Fatalf("pause completion = %+v", got)
	}
	if got := classifyRunCompletion(errors.New("provider failed")); got.outcome != "" || !got.isError || got.exitCode != 1 {
		t.Fatalf("error completion = %+v", got)
	}
	if got := classifyRunCompletion(nil); got.outcome != "" || got.isError || got.exitCode != 0 {
		t.Fatalf("success completion = %+v", got)
	}
}
