package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

type largeSchemaTool struct {
	description string
}

func (largeSchemaTool) Name() string            { return "large_schema" }
func (t largeSchemaTool) Description() string   { return t.description }
func (largeSchemaTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (largeSchemaTool) ReadOnly() bool          { return true }
func (largeSchemaTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestOutputBudgetReserveScalesWithWindow(t *testing.T) {
	tests := []struct {
		window int
		want   int
	}{
		{window: 0, want: 8 * 1024},
		{window: 20_000, want: 256},
		{window: 128_000, want: 1_000},
		{window: 1_048_576, want: 8 * 1024},
		{window: 2_000_000, want: 8 * 1024},
	}
	for _, tt := range tests {
		if got := outputBudgetReserveForWindow(tt.window); got != tt.want {
			t.Fatalf("window %d reserve = %d, want %d", tt.window, got, tt.want)
		}
	}
}

func TestPrepareSamplingRequestAdmitsCompactedSmallWindowDeadZone(t *testing.T) {
	const window = 20_000
	prov := &overflowSummaryProvider{}
	reg := tool.NewRegistry()
	reg.Add(largeSchemaTool{description: strings.Repeat("large deterministic schema. ", 1200)})
	sess := foldableSessionOverForce(20)
	a := New(prov, reg, sess, Options{
		ContextWindow:   window,
		CompactRatio:    defaultCompactRatio,
		MaxOutputTokens: 8192,
	}, event.Discard)

	before := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	if before < a.compactTrigger() || before >= a.hardInputCeiling() {
		t.Fatalf("fixture prompt = %d, want pressure range [%d, %d)", before, a.compactTrigger(), a.hardInputCeiling())
	}

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(prov.requests))
	}
	adm := a.lastAdmission()
	if adm.PromptTokens <= window-outputBudgetReserve {
		t.Fatalf("fixture missed former admission dead zone: prompt = %d, old ceiling = %d", adm.PromptTokens, window-outputBudgetReserve)
	}
	if adm.ReserveTokens != outputBudgetReserveForWindow(window) {
		t.Fatalf("reserve = %d, want scaled %d", adm.ReserveTokens, outputBudgetReserveForWindow(window))
	}
	if prepared.req.MaxTokens <= 0 || prepared.req.MaxTokens+adm.PromptTokens+adm.ReserveTokens > window {
		t.Fatalf("prepared request does not fit: prompt=%d output=%d reserve=%d window=%d",
			adm.PromptTokens, prepared.req.MaxTokens, adm.ReserveTokens, window)
	}
}
