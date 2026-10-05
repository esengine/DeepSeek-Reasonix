package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"

	"reasonix/internal/contract/agentgraph"

	"reasonix/internal/base/testenv"
	"slices"
)

func TestUpstreamNoteOmitsTheBlockWhenNothingWasDelivered(t *testing.T) {
	if note := renderUpstreamNote(nil, upstreamBudgetBytes); note != "" {
		t.Errorf("no dependencies must render nothing, got %q", note)
	}
	blank := []UpstreamResult{{ID: "a", Answer: "  \n\t "}, {ID: "b"}}
	if note := renderUpstreamNote(blank, upstreamBudgetBytes); note != "" {
		t.Errorf("dependencies that completed saying nothing must render nothing, got %q", note)
	}
}

func TestUpstreamNoteCarriesEveryDependencyAndNamesIt(t *testing.T) {
	note := renderUpstreamNote([]UpstreamResult{
		{ID: "research", Answer: "the parser lives in parse.go"},
		{ID: "survey", Answer: "no callers outside cmd/"},
	}, upstreamBudgetBytes)
	for _, want := range []string{
		"research", "the parser lives in parse.go",
		"survey", "no callers outside cmd/",
		"not instructions", // a sibling's answer is data, never a command
	} {
		if !strings.Contains(note, want) {
			t.Errorf("upstream note is missing %q:\n%s", want, note)
		}
	}
}

// One budget is shared, so a wide fan-in cannot push the opening turn past a
// size the caller can predict, and no single dependency is starved out of it.
func TestUpstreamNoteSplitsOneBudgetAcrossDependencies(t *testing.T) {
	const budget = 1024
	long := strings.Repeat("x", 8*budget)
	deps := []UpstreamResult{
		{ID: "a", Answer: long}, {ID: "b", Answer: long},
		{ID: "c", Answer: long}, {ID: "d", Answer: long},
	}
	note := renderUpstreamNote(deps, budget)
	// Only the answers and the truncation marker's "context" spell x.
	if delivered := strings.Count(note, "x") - len(deps); delivered > budget {
		t.Errorf("delivered %d answer bytes, want at most the %d-byte shared budget:\n%s", delivered, budget, note)
	}
	for _, dep := range deps {
		if !strings.Contains(note, "from "+dep.ID) {
			t.Errorf("dependency %q was starved out of the block:\n%s", dep.ID, note)
		}
	}
	if !strings.Contains(note, "truncated to fit") {
		t.Errorf("a clipped answer must say so:\n%s", note)
	}
}

func TestUpstreamForReadsDependenciesInDeclarationOrder(t *testing.T) {
	plan, err := planFor(t,
		fleetTaskItem{ID: "research"},
		fleetTaskItem{ID: "survey"},
		fleetTaskItem{ID: "implement", DependsOn: []string{"survey", "research"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	results := []fleetItemResult{
		{index: 0, status: agentgraph.StateCompleted, output: "R"},
		{index: 1, status: agentgraph.StateCompleted, output: "S"},
		{index: 2, status: agentgraph.StatePending},
	}
	got := plan.upstreamFor(2, results)
	want := []UpstreamResult{{ID: "survey", Answer: "S"}, {ID: "research", Answer: "R"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("upstreamFor = %+v, want depends_on order %+v", got, want)
	}
	if up := plan.upstreamFor(0, results); up != nil {
		t.Errorf("a root has no dependencies to start from, got %+v", up)
	}
}

// The effect at its final boundary: an upstream answer reaches the dependent
// child's opening provider turn, a root's turn carries no such block, and the
// persisted capsule records which of the two started from inherited context.
func TestFleetDeliversUpstreamAnswerToTheDependentChild(t *testing.T) {
	const probe = "ARTIFACT-42 lives in parse.go"
	root := testenv.TempDir(t)
	prov := &upstreamProbeProvider{answer: probe}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{}) // the read-only item needs a non-empty registry
	store := mustSubagentStore(t)
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4))
	fleet := NewFleetTool(task)
	ctx := agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	ctx = agent.WithParentSession(ctx, "upstream-parent")

	out, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"TASK-ALPHA survey the parser","read_only":true},
		{"id":"implement","prompt":"TASK-BETA rewrite the parser","depends_on":["research"],"write_paths":["parse.go"]}
	]}`))
	if err != nil {
		t.Fatalf("fleet: %v\n%s", err, out)
	}

	downstream := prov.turnFor("TASK-BETA")
	if downstream == "" {
		t.Fatal("the dependent never ran")
	}
	for _, want := range []string{"<upstream-results", "from research", probe} {
		if !strings.Contains(downstream, want) {
			t.Errorf("the dependent's opening turn is missing %q:\n%s", want, downstream)
		}
	}
	if upstream := prov.turnFor("TASK-ALPHA"); strings.Contains(upstream, "<upstream-results") {
		t.Errorf("a root depends on nothing and must start from no upstream block:\n%s", upstream)
	}

	// The record, not just the turn: a reader asking why this writer knew about
	// ARTIFACT-42 gets the answer from the sidecar.
	inherited := map[string][]string{}
	for _, ref := range subagentRefsIn(out) {
		meta, err := store.LoadMeta(ref)
		if err != nil {
			t.Fatalf("load meta %q: %v", ref, err)
		}
		inherited[meta.ParentToolCallID] = meta.Capsule.Inherited.UpstreamFrom
	}
	// Which dependency, not whether one: the sidecar names the run this writer
	// read, which is what a bool in its place could never answer.
	want := map[string][]string{"fleet-call/fleet-1": nil, "fleet-call/fleet-2": {"research"}}
	for id, wantSources := range want {
		got, ok := inherited[id]
		if !ok {
			t.Fatalf("no persisted capsule for %s; recorded %+v", id, inherited)
		}
		if !slices.Equal(got, wantSources) {
			t.Errorf("%s capsule inherited.upstreamFrom = %v, want %v", id, got, wantSources)
		}
	}
}

// The measurement arm: the edge still orders its endpoints and still skips a
// broken branch, but delivers nothing — which is what lets a benchmark charge a
// difference to the payload rather than to the ordering.
func TestUpstreamAblationOrdersWithoutDelivering(t *testing.T) {
	const probe = "ARTIFACT-42 lives in parse.go"
	root := testenv.TempDir(t)
	prov := &upstreamProbeProvider{answer: probe}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4)).
		WithAblation(ablation.New(ablation.Upstream))
	fleet := NewFleetTool(task)
	ctx := agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false)

	out, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"TASK-ALPHA survey the parser","read_only":true},
		{"id":"implement","prompt":"TASK-BETA rewrite the parser","depends_on":["research"],"write_paths":["parse.go"]}
	]}`))
	if err != nil {
		t.Fatalf("fleet: %v\n%s", err, out)
	}

	downstream := prov.turnFor("TASK-BETA")
	if downstream == "" {
		t.Fatal("the ablated arm must still run the dependent, only without its input")
	}
	for _, unwanted := range []string{"<upstream-results", probe} {
		if strings.Contains(downstream, unwanted) {
			t.Errorf("the no-upstream arm delivered %q:\n%s", unwanted, downstream)
		}
	}
}

func subagentRefsIn(aggregate string) []string {
	const prefix = "Subagent reference: "
	var refs []string
	for line := range strings.SplitSeq(aggregate, "\n") {
		if ref, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			refs = append(refs, strings.TrimSpace(ref))
		}
	}
	return refs
}

// upstreamProbeProvider records the opening user turn of each fleet item and
// answers the root with a probe the dependent can only know if the edge carried
// it. Items are told apart by the TASK- token in their own prompt, so matching
// the dependent first keeps the delivered upstream text from selecting the root.
type upstreamProbeProvider struct {
	answer string
	mu     sync.Mutex
	turns  map[string]string
}

func (p *upstreamProbeProvider) Name() string { return "upstream-probe" }

func (p *upstreamProbeProvider) turnFor(token string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turns[token]
}

func (p *upstreamProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	last := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			last = m.Content
		}
	}
	answer := "done"
	p.mu.Lock()
	if p.turns == nil {
		p.turns = map[string]string{}
	}
	switch {
	case strings.Contains(last, "TASK-BETA"):
		if _, seen := p.turns["TASK-BETA"]; !seen {
			p.turns["TASK-BETA"] = last
		}
	case strings.Contains(last, "TASK-ALPHA"):
		if _, seen := p.turns["TASK-ALPHA"]; !seen {
			p.turns["TASK-ALPHA"] = last
		}
		answer = p.answer
	}
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: answer}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
