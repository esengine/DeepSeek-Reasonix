package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// --- toWire ---

func TestToWireText(t *testing.T) {
	e := event.Event{Kind: event.Text, Text: "hello"}
	w := toWire(e)
	if w.Kind != "text" || w.Text != "hello" {
		t.Errorf("text wire = %+v", w)
	}
}

func TestToWireReasoning(t *testing.T) {
	e := event.Event{Kind: event.Reasoning, Text: "thinking..."}
	w := toWire(e)
	if w.Kind != "reasoning" || w.Text != "thinking..." {
		t.Errorf("reasoning wire = %+v", w)
	}
}

func TestToWireNoticeInfo(t *testing.T) {
	e := event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "info"}
	w := toWire(e)
	if w.Kind != "notice" || w.Level != "info" {
		t.Errorf("notice info = %+v", w)
	}
}

func TestToWireNoticeWarn(t *testing.T) {
	e := event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "warn"}
	w := toWire(e)
	if w.Level != "warn" {
		t.Errorf("notice warn level = %q", w.Level)
	}
}

func TestToWireRetrying(t *testing.T) {
	e := event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10}
	w := toWire(e)
	if w.Kind != "retrying" || w.RetryAttempt != 3 || w.RetryMax != 10 {
		t.Errorf("retrying wire = %+v", w)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if s := string(b); !strings.Contains(s, `"retryAttempt":3`) || !strings.Contains(s, `"retryMax":10`) {
		t.Errorf("retrying JSON = %s", s)
	}
}

func TestToWireToolDispatch(t *testing.T) {
	e := event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "1", Name: "bash", Args: `{"c":"echo"}`, ReadOnly: false}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Name != "bash" || w.Tool.Args != `{"c":"echo"}` {
		t.Errorf("tool dispatch = %+v", w.Tool)
	}
}

func TestToWireToolDispatchProfile(t *testing.T) {
	e := event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "1", Name: "task", Args: `{"prompt":"x"}`,
		Profile: &event.Profile{Model: "deepseek-pro", Effort: "max"},
	}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Profile == nil || w.Tool.Profile.Model != "deepseek-pro" || w.Tool.Profile.Effort != "max" {
		t.Errorf("tool profile = %+v", w.Tool)
	}
}

func TestToWireToolResult(t *testing.T) {
	e := event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "1", Output: "ok", Truncated: true, DurationMs: 522}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Output != "ok" || !w.Tool.Truncated || w.Tool.DurationMs != 522 {
		t.Errorf("tool result = %+v", w.Tool)
	}
}

func TestToWireToolProgress(t *testing.T) {
	e := event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "1", Output: "chunk"}}
	w := toWire(e)
	if w.Kind != "tool_progress" || w.Tool == nil || w.Tool.Output != "chunk" {
		t.Errorf("tool progress = kind:%q tool:%+v", w.Kind, w.Tool)
	}
}

func TestToWireUsage(t *testing.T) {
	e := event.Event{
		Kind:        event.Usage,
		Usage:       &provider.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CacheHitTokens: 80, CacheMissTokens: 20},
		SessionHit:  800,
		SessionMiss: 200,
	}
	w := toWire(e)
	if w.Usage == nil || w.Usage.PromptTokens != 100 || w.Usage.TotalTokens != 150 {
		t.Errorf("usage = %+v", w.Usage)
	}
	if w.Usage.SessionCacheHitTokens != 800 || w.Usage.SessionCacheMissTokens != 200 {
		t.Errorf("session cache = hit:%d miss:%d", w.Usage.SessionCacheHitTokens, w.Usage.SessionCacheMissTokens)
	}
}

func TestToWireUsageWithPricing(t *testing.T) {
	e := event.Event{
		Kind:    event.Usage,
		Usage:   &provider.Usage{CacheHitTokens: 1_000_000, CacheMissTokens: 0, CompletionTokens: 0},
		Pricing: &provider.Pricing{CacheHit: 1.0, Input: 2.0, Output: 10.0},
	}
	w := toWire(e)
	if w.Usage == nil || w.Usage.Cost != 1.0 || w.Usage.CostUSD != 1.0 {
		t.Errorf("cost = %+v, want cost and compat costUsd of 1.0", w.Usage)
	}
	if w.Usage.Currency != "¥" {
		t.Errorf("currency = %q, want ¥", w.Usage.Currency)
	}
}

func TestToWireApprovalRequest(t *testing.T) {
	e := event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "42", Tool: "bash", Subject: "rm"}}
	w := toWire(e)
	if w.Approval == nil || w.Approval.ID != "42" || w.Approval.Tool != "bash" {
		t.Errorf("approval = %+v", w.Approval)
	}
}

func TestToWireAskRequest(t *testing.T) {
	e := event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID:        "ask-1",
		Questions: []event.AskQuestion{{ID: "q1", Header: "Pick", Prompt: "Choose one", Options: []event.AskOption{{Label: "A"}, {Label: "B"}}, Multi: false}},
	}}
	w := toWire(e)
	if w.Ask == nil || w.Ask.ID != "ask-1" {
		t.Errorf("ask = %+v", w.Ask)
	}
	if len(w.Ask.Questions) != 1 || len(w.Ask.Questions[0].Options) != 2 {
		t.Errorf("questions/options = %+v", w.Ask.Questions)
	}
}

func TestToWireTurnDoneWithError(t *testing.T) {
	e := event.Event{Kind: event.TurnDone, Err: errors.New("boom")}
	w := toWire(e)
	if w.Kind != "turn_done" || w.Err != "boom" {
		t.Errorf("turn_done error = %+v", w)
	}
}

func TestToWireTurnDoneNoError(t *testing.T) {
	e := event.Event{Kind: event.TurnDone}
	w := toWire(e)
	if w.Err != "" {
		t.Errorf("turn_done no-error should have empty err, got %q", w.Err)
	}
}

// --- kindNames completeness ---

func TestToWireSteer(t *testing.T) {
	e := event.Event{Kind: event.Steer, Text: "please use smaller diffs"}
	w := toWire(e)
	if w.Kind != "steer" || w.Text != "please use smaller diffs" {
		t.Errorf("steer wire = %+v", w)
	}
}

func TestKindNamesComplete(t *testing.T) {
	// Steer is the last Kind; every value through it must have a wire name,
	// or toWire emits kind:"" and the frontend reducer falls through to undefined.
	for k := event.Kind(0); k <= event.Steer; k++ {
		if kindNames[k] == "" {
			t.Errorf("kind %d has no wire name — toWire would emit kind:\"\"", k)
		}
	}
}

// --- wireEvent JSON round-trip ---

func TestWireEventJSON(t *testing.T) {
	w := wireEvent{Kind: "text", Text: "hello"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded wireEvent
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Kind != "text" || decoded.Text != "hello" {
		t.Errorf("round-trip = %+v", decoded)
	}
}
