package coordinator

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestPlannerSeesWhatTheExecutorDidSinceItLastPlanned(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. use the executor's MCP to list prefabs"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Found Enemy.prefab and Boss.prefab."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Boss.prefab also carries a Script Desc."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Set both values to 40960."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Done again."}, {Type: provider.ChunkDone}},
	}}
	policy := func(_ context.Context, input string) agent.PlannerDecision {
		if strings.HasPrefix(input, "quick") {
			return agent.PlannerDecision{Route: agent.PlannerRouteExecutorOnly, Depth: agent.PlannerDepthNone, Reason: "short_reply"}
		}
		return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull, Reason: "work"}
	}
	executor := agent.New(exec, tool.NewRegistry(), sessionstore.NewSession("exec-sys"), agent.Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner-sys"), nil, nil, agent.Options{}, executor, 0, event.Discard, policy)

	for _, input := range []string{"find the prefabs", "quick: which ones carry Script Desc?", "set the value to 40960", "and once more"} {
		if err := coord.Run(context.Background(), input); err != nil {
			t.Fatalf("Run(%q): %v", input, err)
		}
	}
	if got := len(planner.requests); got != 3 {
		t.Fatalf("planner requests = %d, want 3", got)
	}

	if got := lastUser(planner.requests[0]); strings.Contains(got, "Executor") {
		t.Fatalf("first planning turn owes no executor outcome, got %q", got)
	}
	second := lastUser(planner.requests[1])
	for _, want := range []string{"Found Enemy.prefab and Boss.prefab.", "quick: which ones carry Script Desc?", "Boss.prefab also carries a Script Desc."} {
		if !strings.Contains(second, want) {
			t.Fatalf("second planning turn missing %q:\n%s", want, second)
		}
	}
	third := lastUser(planner.requests[2])
	if !strings.Contains(third, "Set both values to 40960.") {
		t.Fatalf("third planning turn missing the latest executor outcome:\n%s", third)
	}
	if strings.Contains(third, "Found Enemy.prefab") {
		t.Fatalf("an outcome the planner already received was sent again:\n%s", third)
	}
}

func TestPlannerKeepsExecutorOutcomeOwedWhenPlanningFails(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "1. do it"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkError, Err: errors.New("planner overloaded")}},
		{{Type: provider.ChunkText, Text: "1. continue"}, {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "First result."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Second result."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Third result."}, {Type: provider.ChunkDone}},
	}}
	executor := agent.New(exec, tool.NewRegistry(), sessionstore.NewSession("exec-sys"), agent.Options{}, event.Discard)
	coord := NewCoordinator(planner, sessionstore.NewSession("planner-sys"), nil, nil, agent.Options{}, executor, 0, event.Discard, nil)

	for _, input := range []string{"one", "two", "three"} {
		if err := coord.Run(context.Background(), input); err != nil {
			t.Fatalf("Run(%q): %v", input, err)
		}
	}
	third := lastUser(planner.requests[2])
	for _, want := range []string{"First result.", "two", "Second result."} {
		if !strings.Contains(third, want) {
			t.Fatalf("outcome dropped by a failed planning turn, missing %q:\n%s", want, third)
		}
	}
}

// rewritingProvider compacts the executor's session during one stream, the way
// an auto-compaction lands mid-run.
type rewritingProvider struct {
	*mockProvider
	sess     *sessionstore.Session
	rewrites map[int]bool
}

func (p *rewritingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.rewrites[len(p.requests)] {
		p.sess.Rewrite(p.sess.Snapshot(), "test compaction")
	}
	return p.mockProvider.Stream(ctx, req)
}

func TestExecutorOutcomeNeverReportsAnEarlierReplyAfterCompaction(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. next"}, {Type: provider.ChunkDone},
	}}
	execSess := sessionstore.NewSession("exec-sys")
	exec := &rewritingProvider{
		mockProvider: &mockProvider{name: "executor", streams: [][]provider.Chunk{
			{{Type: provider.ChunkText, Text: "Old reply."}, {Type: provider.ChunkDone}},
			{{Type: provider.ChunkError, Err: errors.New("upstream closed the stream")}},
			{{Type: provider.ChunkText, Text: "Planned work done."}, {Type: provider.ChunkDone}},
		}},
		sess:     execSess,
		rewrites: map[int]bool{1: true},
	}
	policy := func(_ context.Context, input string) agent.PlannerDecision {
		if strings.HasPrefix(input, "quick") {
			return agent.PlannerDecision{Route: agent.PlannerRouteExecutorOnly, Depth: agent.PlannerDepthNone, Reason: "short_reply"}
		}
		return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull, Reason: "work"}
	}
	executor := agent.New(exec, tool.NewRegistry(), execSess, agent.Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner-sys"), nil, nil, agent.Options{}, executor, 0, event.Discard, policy)

	_ = coord.Run(context.Background(), "quick one")
	_ = coord.Run(context.Background(), "quick two")
	if err := coord.Run(context.Background(), "now plan"); err != nil {
		t.Fatalf("planned Run: %v", err)
	}
	got := lastUser(planner.requests[0])
	if n := strings.Count(got, "Old reply."); n != 1 {
		t.Fatalf("the first run's reply appears %d times; a compacted run with no reply must not claim it:\n%s", n, got)
	}
	if !strings.Contains(got, "quick two") || !strings.Contains(got, "(no reply captured)") {
		t.Fatalf("the compacted run should be reported without a reply:\n%s", got)
	}
}

func TestOwedExecutorOutcomesStayBounded(t *testing.T) {
	long := strings.Repeat("x", maxExecutorOutcomeRunes+500)
	var streams [][]provider.Chunk
	for range maxOwedExecutorOutcomes + 3 {
		streams = append(streams, []provider.Chunk{{Type: provider.ChunkText, Text: long}, {Type: provider.ChunkDone}})
	}
	exec := &mockProvider{name: "executor", streams: streams}
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. next"}, {Type: provider.ChunkDone},
	}}
	policy := func(context.Context, string) agent.PlannerDecision {
		return agent.PlannerDecision{Route: agent.PlannerRouteExecutorOnly, Depth: agent.PlannerDepthNone, Reason: "short_reply"}
	}
	executor := agent.New(exec, tool.NewRegistry(), sessionstore.NewSession("exec-sys"), agent.Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner-sys"), nil, nil, agent.Options{}, executor, 0, event.Discard, policy)

	for i := range maxOwedExecutorOutcomes + 3 {
		if err := coord.Run(context.Background(), "quick "+strconv.Itoa(i)); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}
	if n := len(coord.owed.outcomes); n != maxOwedExecutorOutcomes {
		t.Fatalf("owed outcomes held = %d, want %d", n, maxOwedExecutorOutcomes)
	}
	for _, o := range coord.owed.outcomes {
		if n := len([]rune(o.reply)); n > maxExecutorOutcomeRunes+64 {
			t.Fatalf("held reply is %d runes, want it cut at append", n)
		}
	}
	got := coord.withExecutorOutcomes("task")
	if !strings.Contains(got, "(3 earlier executor turn(s) omitted)") || !strings.Contains(got, "cut by the host: 500 more characters") {
		t.Fatalf("rendered projection does not say what it dropped:\n%s", got)
	}
}
