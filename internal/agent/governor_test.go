package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestApplyGovernorStampsEligibilityWithoutEngagingByDefault(t *testing.T) {
	a := &Agent{sink: &ebmSink{}, lastReasoning: govReasoningThreshold}
	sample := evidence.OutcomeSample{}
	a.applyGovernor(&sample)
	if !sample.GovernorEligible || sample.GovernorEngaged {
		t.Fatalf("sample = %+v, want eligible without engagement (experiment off)", sample)
	}
	if a.governorOverride() != "" {
		t.Fatalf("baseline arm must not override effort, got %q", a.governorOverride())
	}
}

func TestApplyGovernorEngagesAndExitsWhenEnabled(t *testing.T) {
	old := governorEnabled
	governorEnabled = true
	defer func() { governorEnabled = old }()

	sink := &ebmSink{}
	a := &Agent{sink: sink, lastReasoning: govReasoningThreshold}

	cheap := evidence.OutcomeSample{}
	a.lastReasoning = govReasoningThreshold - 1
	a.applyGovernor(&cheap)
	if cheap.GovernorEligible || cheap.GovernorEngaged {
		t.Fatalf("cheap round = %+v, want untouched", cheap)
	}

	a.lastReasoning = govReasoningThreshold
	sample := evidence.OutcomeSample{}
	a.applyGovernor(&sample)
	if !sample.GovernorEngaged || a.governorOverride() != governorEffort {
		t.Fatalf("sample = %+v override = %q, want engaged with %q", sample, a.governorOverride(), governorEffort)
	}
	if len(sink.notices) != 1 || sink.notices[0].Code != event.NoticeCodeReasoningGovernor {
		t.Fatalf("notices = %+v, want one reasoning_governor", sink.notices)
	}

	// Exploration persists: cheap rounds alone do not disengage.
	a.lastReasoning = 0
	quiet := evidence.OutcomeSample{}
	a.applyGovernor(&quiet)
	if !quiet.GovernorEngaged {
		t.Fatalf("quiet round = %+v, want still engaged", quiet)
	}

	// Debt opening (a mutation) stands the override down immediately.
	debt := evidence.OutcomeSample{DebtAge: 1}
	a.applyGovernor(&debt)
	if debt.GovernorEngaged || a.governorOverride() != "" {
		t.Fatalf("debt round = %+v override = %q, want disengaged", debt, a.governorOverride())
	}
	if len(sink.notices) != 1 {
		t.Fatalf("notices = %+v, want the engage notice only once per turn", sink.notices)
	}

	a.resetTurnEvidence()
	if a.governor.engaged || a.governor.noticed {
		t.Fatalf("governor = %+v, want reset with the turn", a.governor)
	}
}

func TestGovernorExitOnLocalExecAndDiscriminating(t *testing.T) {
	if !governorExit(evidence.OutcomeSample{LocalExecSeen: true}) {
		t.Fatal("local execution must exit the governor")
	}
	if !governorExit(evidence.OutcomeSample{Discriminating: 1}) {
		t.Fatal("a discriminating observation must exit the governor")
	}
	if governorExit(evidence.OutcomeSample{}) {
		t.Fatal("pure exploration must not exit the governor")
	}
}

// The effect boundary: an expensive exploration round must put the reduced
// depth on the NEXT provider request, and only under the experiment arm.
func TestGovernorOverrideReachesProviderRequest(t *testing.T) {
	old := governorEnabled
	governorEnabled = true
	defer func() { governorEnabled = old }()

	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	expensive := provider.Chunk{Type: provider.ChunkUsage,
		Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 30, ReasoningTokens: govReasoningThreshold, TotalTokens: 40}}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":["q":1]}`), expensive, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "explore"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("requests = %d, want the tool round and the follow-up", len(prov.requests))
	}
	if got := prov.requests[0].EffortOverride; got != "" {
		t.Fatalf("first request override = %q, want empty before any observation", got)
	}
	if got := prov.requests[1].EffortOverride; got != governorEffort {
		t.Fatalf("post-exploration request override = %q, want %q", got, governorEffort)
	}

	governorEnabled = false
	prov2 := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":["q":1]}`), expensive, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	b := New(prov2, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	_ = b.Run(context.Background(), "explore")
	for i, req := range prov2.requests {
		if req.EffortOverride != "" {
			t.Fatalf("baseline arm request %d carries override %q", i, req.EffortOverride)
		}
	}
}
