package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
	sink.Emit(event.Event{Kind: event.Usage, UsageModel: "deepseek", Usage: &provider.Usage{
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
	if result.SchemaVersion != 2 || result.Type != "result" || result.Subtype != "success" || result.IsError || result.Result != "done" || result.SessionID != "abc" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 || result.Usage.CacheReadInputTokens != 8 || result.Usage.CacheCreationInputTokens != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestRunOutputJSONUnknownPricingFailsClosed(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Text: "done"})
	sink.Emit(event.Event{Kind: event.Usage, UsageModel: "deepseek", Usage: &provider.Usage{
		PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 80, CacheMissTokens: 20,
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}
	if _, ok := result["total_cost_usd"]; ok {
		t.Fatalf("unknown pricing must omit total_cost_usd: %s", out.String())
	}
	if result["schema_version"] != float64(2) || result["cost_is_partial"] != true {
		t.Fatalf("v2 fail-closed fields missing: %s", out.String())
	}
	if _, ok := result["total_cost_usd_ticks"]; ok {
		t.Fatalf("unknown pricing must omit total_cost_usd_ticks: %s", out.String())
	}
	if _, ok := result["modelUsage"].(map[string]any)["executor:deepseek"]; !ok {
		t.Fatalf("model attribution missing: %s", out.String())
	}
}

func TestRunOutputJSONCompleteUSDIncludesExactTicks(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Usage, UsageSource: event.UsageSourcePlanner, UsageModel: "planner", Usage: &provider.Usage{PromptTokens: 3, CompletionTokens: 1, CacheMissTokens: 3}, Pricing: &provider.Pricing{Input: 0.1, Output: 0.2, Currency: "USD"}})
	sink.Emit(event.Event{Kind: event.Usage, UsageSource: event.UsageSourceExecutor, UsageModel: "executor", Usage: &provider.Usage{PromptTokens: 7, CompletionTokens: 2, CacheHitTokens: 7}, Pricing: &provider.Pricing{CacheHit: 0.01, Output: 0.2, Currency: "USD"}})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.CostIsPartial || result.TotalCostUSDTicks == nil || *result.TotalCostUSDTicks != 9700 || result.TotalCostUSD == nil || *result.TotalCostUSD != 0.00000097 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunOutputJSONReportsOpenBackgroundSubagent(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{
		ID: "task-1", Kind: "task", Status: "running",
	}})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.UsageIsIncomplete || result.OpenBackgroundSubagents != 1 || len(result.IncompleteReasons) != 2 {
		t.Fatalf("result = %+v", result)
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
