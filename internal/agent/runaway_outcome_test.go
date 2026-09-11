package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type scriptedTool struct {
	name     string
	readOnly bool
}

func (s scriptedTool) Name() string        { return s.name }
func (s scriptedTool) Description() string { return s.name }
func (s scriptedTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"command":{"type":"string"}}}`)
}
func (s scriptedTool) ReadOnly() bool { return s.readOnly }
func (s scriptedTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok: 12 tests passed\n", nil
}

type scriptedCall struct {
	tool string
	args string
}

// seriesProvider replays a fixed sequence of tool rounds, one per model round,
// then finishes. It is the smallest thing that drives real receipts through the
// real run loop.
type seriesProvider struct {
	calls []scriptedCall
	round atomic.Int32
}

func (p *seriesProvider) Name() string { return "scripted" }

func (p *seriesProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	i := int(p.round.Add(1)) - 1
	ch := make(chan provider.Chunk, 4)
	if i >= len(p.calls) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Done."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	call := p.calls[i]
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "收到。先收集当前真实状态。"}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID: fmt.Sprintf("call-%d", i), Name: call.tool, Arguments: call.args,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func outcomeSeries(t *testing.T, calls []scriptedCall) []evidence.OutcomeSample {
	t.Helper()
	sink := &outcomeSeriesSink{}
	reg := tool.NewRegistry()
	reg.Add(scriptedTool{name: "read_file", readOnly: true})
	reg.Add(scriptedTool{name: "edit_file"})
	reg.Add(scriptedTool{name: "bash"})
	prov := &seriesProvider{calls: calls}
	a := New(prov, reg, NewSession("sys"), Options{MaxSteps: 0}, sink)
	if err := a.Run(context.Background(), "collect the real state, then rewrite HANDOVER.md"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return sink.samples
}

type outcomeSeriesSink struct{ samples []evidence.OutcomeSample }

func (s *outcomeSeriesSink) Emit(event.Event) {}
func (s *outcomeSeriesSink) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	s.samples = append(s.samples, sample)
}

// The measurement the promotion decision needs: run the incident's shape and a
// real working turn through the same loop, and print what each scorer saw. The
// legacy gain is one number; the outcome sample is a decomposition, and the
// question is whether the decomposition separates them where the number cannot.
func TestOutcomeVersusLegacyOnTheRunawayShape(t *testing.T) {
	wandering := make([]scriptedCall, 0, 8)
	for i := range 8 {
		wandering = append(wandering, scriptedCall{"read_file", fmt.Sprintf(`{"path":"internal/pkg%d/file.go"}`, i)})
	}
	realWork := []scriptedCall{
		{"read_file", `{"path":"internal/agent/agent.go"}`},
		{"read_file", `{"path":"internal/agent/run_loop.go"}`},
		{"edit_file", `{"path":"internal/agent/run_loop.go"}`},
		{"bash", `{"command":"go test ./internal/agent/"}`},
		{"edit_file", `{"path":"internal/agent/agent.go"}`},
		{"bash", `{"command":"go test ./internal/agent/"}`},
	}

	for _, scenario := range []struct {
		name  string
		calls []scriptedCall
	}{{"wandering (the incident)", wandering}, {"real work", realWork}} {
		samples := outcomeSeries(t, scenario.calls)
		t.Logf("\n=== %s ===", scenario.name)
		t.Logf("%-6s %-6s %-6s %-6s %-6s %-6s %-6s %-6s", "round", "legacy", "explor", "verify", "object", "churn", "discrim", "debt")
		for _, s := range samples {
			t.Logf("%-6d %-6d %-6d %-6d %-6d %-6d %-6d %-6d",
				s.Round, s.LegacyGain, s.Exploration, s.Verification, s.Objective, s.Churn, s.Discriminating, s.DebtAge)
		}
	}

	// Exploration counts while it might still lead somewhere and then stops, so
	// the ladder can finally climb. Both halves matter: never decaying is the
	// runaway, decaying at once would nudge ordinary investigation.
	scored, decayed := 0, 0
	for i, s := range outcomeSeries(t, wandering) {
		if s.Exploration == 0 || s.Discriminating != 0 || s.Churn != 0 || s.Objective != 0 {
			t.Fatalf("wandering round %d = %+v; the shape under test is exploration and nothing else", i+1, s)
		}
		if s.LegacyGain > 0 {
			scored++
			continue
		}
		decayed++
	}
	if scored == 0 {
		t.Fatal("exploration stopped counting immediately; ordinary investigation would be nudged on arrival")
	}
	if decayed == 0 {
		t.Fatal("a look-only run never stopped counting as progress; the ladder can never climb")
	}
	// The decomposition can: real work reaches a discriminating observation,
	// and never spends more than two rounds on exploration alone.
	discriminating, exploreRun, longestExploreRun := 0, 0, 0
	for _, s := range outcomeSeries(t, realWork) {
		discriminating += s.Discriminating
		if s.Exploration > 0 && s.Discriminating == 0 && s.Churn == 0 {
			exploreRun++
			longestExploreRun = max(longestExploreRun, exploreRun)
			continue
		}
		exploreRun = 0
	}
	if discriminating == 0 {
		t.Fatal("real work produced no discriminating observation; the contrast is meaningless")
	}
	if longestExploreRun >= len(wandering) {
		t.Fatalf("real work explored for %d rounds straight; it no longer separates from the runaway", longestExploreRun)
	}
}
