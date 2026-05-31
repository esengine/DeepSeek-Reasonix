package main

import (
	"encoding/json"
	"errors"
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

func TestToWireToolDispatch(t *testing.T) {
	e := event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "1", Name: "bash", Args: `{"c":"echo"}`, ReadOnly: false}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Name != "bash" || w.Tool.Args != `{"c":"echo"}` {
		t.Errorf("tool dispatch = %+v", w.Tool)
	}
}

func TestToWireToolResult(t *testing.T) {
	e := event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "1", Output: "ok", Truncated: true}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Output != "ok" || !w.Tool.Truncated {
		t.Errorf("tool result = %+v", w.Tool)
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
	if w.Usage == nil || w.Usage.CostUSD != 1.0 {
		t.Errorf("cost = %f, want 1.0", w.Usage.CostUSD)
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

func TestKindNamesComplete(t *testing.T) {
	allKinds := []event.Kind{
		event.TurnStarted, event.Reasoning, event.Text, event.Message,
		event.ToolDispatch, event.ToolResult, event.Usage, event.Notice,
		event.Phase, event.ApprovalRequest, event.AskRequest, event.TurnDone,
	}
	for _, k := range allKinds {
		if _, ok := kindNames[k]; !ok {
			t.Errorf("kind %d not in kindNames", k)
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
