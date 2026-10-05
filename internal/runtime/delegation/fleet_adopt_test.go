package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentgraph"
)

func TestFleetItemShapeRequiresExactlyOneOfPromptAndAdoptRef(t *testing.T) {
	for name, tc := range map[string]struct {
		item fleetTaskItem
		want string
	}{
		"neither":            {fleetTaskItem{}, "prompt is required"},
		"both":               {fleetTaskItem{Prompt: "do it", AdoptRef: "sa_x"}, "mutually exclusive"},
		"adopted profile":    {fleetTaskItem{AdoptRef: "sa_x", Profile: "review"}, "profile is not valid"},
		"adopted writes":     {fleetTaskItem{AdoptRef: "sa_x", WritePaths: []string{"a.go"}}, "write_paths is not valid"},
		"adopted tools":      {fleetTaskItem{AdoptRef: "sa_x", Tools: []string{"read_file"}}, "tools is not valid"},
		"adopted steps":      {fleetTaskItem{AdoptRef: "sa_x", MaxSteps: 3}, "max_steps is not valid"},
		"adopted model":      {fleetTaskItem{AdoptRef: "sa_x", Model: "other"}, "model is not valid"},
		"adopted effort":     {fleetTaskItem{AdoptRef: "sa_x", Effort: "max"}, "effort is not valid"},
		"adopted read_only":  {fleetTaskItem{AdoptRef: "sa_x", ReadOnly: true}, "read_only is not valid"},
		"plain prompt is ok": {fleetTaskItem{Prompt: "do it", Profile: "review"}, ""},
		"plain adopt is ok":  {fleetTaskItem{AdoptRef: "sa_x", DependsOn: []string{"a"}, Description: "label"}, ""},
	} {
		err := validateFleetItemShape(0, tc.item)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected err %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err = %v, want one mentioning %q", name, err, tc.want)
		}
	}
}

// An adopted node is already resolved, so its dependents must be ready on the
// first pass: nothing will ever publish a result for it.
func TestUnresolvedDepCountsTreatsAdoptedAsSettled(t *testing.T) {
	plan, err := planFor(t,
		fleetTaskItem{ID: "research"},
		fleetTaskItem{ID: "implement", DependsOn: []string{"research"}},
		fleetTaskItem{ID: "review", DependsOn: []string{"implement"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	results := []fleetItemResult{
		{index: 0, status: agentgraph.StateAdopted, output: "prior research"},
		{index: 1, status: agentgraph.StatePending},
		{index: 2, status: agentgraph.StatePending},
	}
	pending := plan.unresolvedDepCounts(results)
	if pending[1] != 0 {
		t.Errorf("implement still waits on %d deps; an adopted dependency has settled", pending[1])
	}
	if pending[2] != 1 {
		t.Errorf("review waits on %d deps, want the still-pending implement", pending[2])
	}
	if got := ready(pending); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("ready = %v, want the adopted node (launch filters it) and implement", got)
	}
	if up := plan.upstreamFor(1, results); len(up) != 1 || up[0].Answer != "prior research" {
		t.Errorf("upstreamFor = %+v, want the adopted answer to ride the edge", up)
	}
}

func newAdoptionFleet(t *testing.T, store *SubagentStore, root string, prov *upstreamProbeProvider) *FleetTool {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	return NewFleetTool(NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4)))
}

func adoptionCtx(sink event.Sink) context.Context {
	return agent.WithParentSession(agent.WithCallContext(context.Background(), "fleet-call", sink, nil, false), "adopt-parent")
}

// The whole point: a fleet re-issued after an interruption stands its finished
// item down to a reference, and the data edge carries that answer forward
// exactly as if the item had just produced it.
func TestFleetAdoptsPriorAnswerAndFeedsItDownstream(t *testing.T) {
	const probe = "ARTIFACT-42 lives in parse.go"
	root := testenv.TempDir(t)
	store := mustSubagentStore(t)
	ctx := adoptionCtx(event.Discard)

	first := newAdoptionFleet(t, store, root, &upstreamProbeProvider{answer: probe})
	out, err := first.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"TASK-ALPHA survey the parser","read_only":true},
		{"id":"aside","prompt":"TASK-GAMMA unrelated","read_only":true}
	]}`))
	if err != nil {
		t.Fatalf("first fleet: %v\n%s", err, out)
	}
	refs := subagentRefsIn(out)
	if len(refs) != 2 {
		t.Fatalf("expected a reference per child, got %v\n%s", refs, out)
	}
	researchRef := refs[0]

	// A restart would have lost the aggregate; re-issue with the finished item
	// standing down to its reference.
	rerun := &upstreamProbeProvider{answer: "SHOULD-NOT-RUN"}
	second := newAdoptionFleet(t, store, root, rerun)
	out, err = second.Execute(ctx, json.RawMessage(fmt.Sprintf(`{"tasks":[
		{"id":"research","adopt_ref":%q},
		{"id":"implement","prompt":"TASK-BETA rewrite the parser","depends_on":["research"],"write_paths":["parse.go"]}
	]}`, researchRef)))
	if err != nil {
		t.Fatalf("second fleet: %v\n%s", err, out)
	}

	if ran := rerun.turnFor("TASK-ALPHA"); ran != "" {
		t.Errorf("the adopted item was run again:\n%s", ran)
	}
	downstream := rerun.turnFor("TASK-BETA")
	if downstream == "" {
		t.Fatal("the dependent of an adopted node never started")
	}
	for _, want := range []string{"<upstream-results", "from research", probe} {
		if !strings.Contains(downstream, want) {
			t.Errorf("the dependent's opening turn is missing %q:\n%s", want, downstream)
		}
	}
	for _, want := range []string{"status: adopted", researchRef} {
		if !strings.Contains(out, want) {
			t.Errorf("the aggregate must report the adoption and its reference (%q):\n%s", want, out)
		}
	}
}

// Nothing runs for an adopted node, so it claims nothing: a live writer beside
// it is not a concurrent-writer conflict.
func TestAdoptedItemHoldsNoWriteClaim(t *testing.T) {
	root := testenv.TempDir(t)
	store := mustSubagentStore(t)
	ctx := adoptionCtx(event.Discard)

	first := newAdoptionFleet(t, store, root, &upstreamProbeProvider{answer: "prior"})
	out, err := first.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"TASK-ALPHA a","read_only":true},
		{"id":"two","prompt":"TASK-GAMMA b","read_only":true}
	]}`))
	if err != nil {
		t.Fatalf("seed fleet: %v", err)
	}
	ref := subagentRefsIn(out)[0]

	// The writer omits write_paths, so it claims the whole workspace. A second
	// whole-workspace claim would fail preflight; an adopted node must not add
	// one.
	second := newAdoptionFleet(t, store, root, &upstreamProbeProvider{answer: "x"})
	if _, err := second.Execute(ctx, json.RawMessage(fmt.Sprintf(`{"tasks":[
		{"id":"done","adopt_ref":%q},
		{"id":"writer","prompt":"TASK-BETA rewrite everything"}
	]}`, ref))); err != nil {
		t.Fatalf("an adopted node must not collide with a whole-workspace writer: %v", err)
	}
}

func TestFleetAdoptionPreflightRefusesUnusableReferences(t *testing.T) {
	root := testenv.TempDir(t)
	store := mustSubagentStore(t)
	prov := &upstreamProbeProvider{answer: "prior"}
	fleet := newAdoptionFleet(t, store, root, prov)
	ctx := adoptionCtx(event.Discard)

	out, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"TASK-ALPHA a","read_only":true},
		{"id":"two","prompt":"TASK-GAMMA b","read_only":true}
	]}`))
	if err != nil {
		t.Fatalf("seed fleet: %v", err)
	}
	ref := subagentRefsIn(out)[0]

	for name, tc := range map[string]struct {
		tasks string
		ctx   context.Context
		want  string
	}{
		"unknown reference": {
			tasks: `[{"id":"a","adopt_ref":"sa_nope"},{"id":"b","prompt":"TASK-BETA go"}]`,
			want:  "sa_nope",
		},
		// Fail closed: a parent session whose lineage cannot be walked at all
		// is refused, not given the benefit of the doubt.
		"another conversation": {
			tasks: fmt.Sprintf(`[{"id":"a","adopt_ref":%q},{"id":"b","prompt":"TASK-BETA go"}]`, ref),
			ctx:   agent.WithParentSession(agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false), "someone-else"),
			want:  "ownership could not be verified",
		},
		"every item adopted": {
			tasks: fmt.Sprintf(`[{"id":"a","adopt_ref":%q},{"id":"b","adopt_ref":%q}]`, ref, ref),
			want:  "would run nothing",
		},
	} {
		runCtx := tc.ctx
		if runCtx == nil {
			runCtx = ctx
		}
		reissue := newAdoptionFleet(t, store, root, &upstreamProbeProvider{answer: "x"})
		_, err := reissue.Execute(runCtx, json.RawMessage(`{"tasks":`+tc.tasks+`}`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", name, err, tc.want)
		}
	}
}

// Preflight is the whole gate: a refused adoption starts nothing, matching the
// rest of the fleet's contract.
func TestRefusedAdoptionStartsNothing(t *testing.T) {
	root := testenv.TempDir(t)
	prov := &upstreamProbeProvider{answer: "x"}
	fleet := newAdoptionFleet(t, mustSubagentStore(t), root, prov)

	if _, err := fleet.Execute(adoptionCtx(event.Discard), json.RawMessage(`{"tasks":[
		{"id":"a","adopt_ref":"sa_missing"},
		{"id":"b","prompt":"TASK-BETA go","depends_on":["a"]}
	]}`)); err == nil {
		t.Fatal("an unusable adopt_ref must fail preflight")
	}
	if ran := prov.turnFor("TASK-BETA"); ran != "" {
		t.Errorf("a task ran despite a failed preflight:\n%s", ran)
	}
}
