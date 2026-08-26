package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func finishProtocolAgent(turns ...[]provider.Chunk) (*Agent, *scriptedProvider) {
	prov := &scriptedProvider{name: "finish-protocol", turns: turns}
	reg := tool.NewRegistry()
	reg.Add(NewFinishTool())
	return New(prov, reg, NewSession(""), Options{}, event.Discard), prov
}

func TestStructuredFinishAcceptsVisibleAnswerAndSingleFinalizer(t *testing.T) {
	a, prov := finishProtocolAgent([]provider.Chunk{
		{Type: provider.ChunkText, Text: "The requested work is complete."},
		toolCallChunk("finish-1", "finish", `{"outcome":"completed"}`),
		{Type: provider.ChunkDone},
	})

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
	if got := a.TurnFinishOutcome(); got != FinishCompleted {
		t.Fatalf("finish outcome = %q, want %q", got, FinishCompleted)
	}
}

func TestStructuredFinishRepairsMissingFinalizerOnce(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{{Type: provider.ChunkText, Text: "The requested work is complete."}, {Type: provider.ChunkDone}},
		[]provider.Chunk{toolCallChunk("finish-1", "finish", `{"outcome":"partial"}`), {Type: provider.ChunkDone}},
	)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one repair round", prov.call)
	}
	if got := a.TurnFinishOutcome(); got != FinishPartial {
		t.Fatalf("finish outcome = %q, want %q", got, FinishPartial)
	}
}

func TestStructuredFinishRepairsMissingVisibleAnswerWithoutDuplicateFinish(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{toolCallChunk("finish-1", "finish", `{"outcome":"blocked"}`), {Type: provider.ChunkDone}},
		[]provider.Chunk{{Type: provider.ChunkText, Text: "Progress is blocked on an external dependency."}, {Type: provider.ChunkDone}},
	)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one repair round", prov.call)
	}
	if got := a.TurnFinishOutcome(); got != FinishBlocked {
		t.Fatalf("finish outcome = %q, want %q", got, FinishBlocked)
	}
}

func TestStructuredFinishMissingTwiceIsProtocolFailed(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{{Type: provider.ChunkText, Text: "I should ask the user something."}, {Type: provider.ChunkDone}},
		[]provider.Chunk{{Type: provider.ChunkText, Text: "Still no ask or finish call."}, {Type: provider.ChunkDone}},
	)

	err := a.Run(context.Background(), "do the work")
	if !IsProtocolFailed(err) {
		t.Fatalf("Run error = %v, want ProtocolFailedError", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want exactly one repair", prov.call)
	}
}

func TestStructuredFinishMixedToolBatchGetsOneRepair(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{
			{Type: provider.ChunkText, Text: "The work is complete."},
			toolCallChunk("finish-mixed", "finish", `{"outcome":"completed"}`),
			toolCallChunk("extra", "read_file", `{"path":"late.txt"}`),
			{Type: provider.ChunkDone},
		},
		[]provider.Chunk{toolCallChunk("finish-only", "finish", `{"outcome":"completed"}`), {Type: provider.ChunkDone}},
	)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 || a.TurnFinishOutcome() != FinishCompleted {
		t.Fatalf("calls=%d outcome=%q, want one repair and completed", prov.call, a.TurnFinishOutcome())
	}
}

func TestStructuredFinishInvalidOutcomeGetsOneRepair(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{
			{Type: provider.ChunkText, Text: "The work is partially complete."},
			toolCallChunk("finish-invalid", "finish", `{"outcome":"unknown"}`),
			{Type: provider.ChunkDone},
		},
		[]provider.Chunk{toolCallChunk("finish-valid", "finish", `{"outcome":"partial"}`), {Type: provider.ChunkDone}},
	)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 || a.TurnFinishOutcome() != FinishPartial {
		t.Fatalf("calls=%d outcome=%q, want one repair and partial", prov.call, a.TurnFinishOutcome())
	}
}

func TestStructuredFinishDuplicateAfterRepairIsProtocolFailed(t *testing.T) {
	a, prov := finishProtocolAgent(
		[]provider.Chunk{toolCallChunk("finish-first", "finish", `{"outcome":"blocked"}`), {Type: provider.ChunkDone}},
		[]provider.Chunk{
			{Type: provider.ChunkText, Text: "The work is blocked."},
			toolCallChunk("finish-second", "finish", `{"outcome":"blocked"}`),
			{Type: provider.ChunkDone},
		},
	)

	if err := a.Run(context.Background(), "do the work"); !IsProtocolFailed(err) {
		t.Fatalf("Run error = %v, want duplicate finish protocol failure", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want exactly one repair", prov.call)
	}
}

func TestPureTextProviderCompatibilityWithoutFinishTool(t *testing.T) {
	prov := &scriptedProvider{name: "text-only", turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "A plain compatible answer."},
		{Type: provider.ChunkDone},
	}}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
}

type textOnlyScriptedProvider struct{ *scriptedProvider }

func (*textOnlyScriptedProvider) SupportsTools() bool { return false }

func TestPureTextProviderCapabilityHidesFinishAndKeepsCompatibility(t *testing.T) {
	base := &scriptedProvider{name: "text-only-capability", turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "A text-only provider answer."},
		{Type: provider.ChunkDone},
	}}}
	prov := &textOnlyScriptedProvider{scriptedProvider: base}
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	reg.Add(NewFinishTool())
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(base.requests) != 1 || len(base.requests[0].Tools) != 0 {
		t.Fatalf("text-only request tools = %#v, want a non-nil empty surface", base.requests)
	}
}
