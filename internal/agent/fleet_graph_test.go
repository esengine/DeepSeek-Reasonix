package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func planFor(t *testing.T, items ...fleetTaskItem) (fleetPlan, error) {
	t.Helper()
	return newFleetPlan(items, false)
}

func TestFleetPlanRejectsBrokenGraphsBeforeAnythingRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		items []fleetTaskItem
		want  string
	}{
		"duplicate id": {
			items: []fleetTaskItem{{ID: "a"}, {ID: "a"}},
			want:  "already used",
		},
		"unknown dependency": {
			items: []fleetTaskItem{{ID: "a"}, {ID: "b", DependsOn: []string{"nope"}}},
			want:  "matches no task id",
		},
		"self dependency": {
			items: []fleetTaskItem{{ID: "a", DependsOn: []string{"a"}}, {ID: "b"}},
			want:  "depends_on itself",
		},
		"cycle": {
			items: []fleetTaskItem{
				{ID: "a", DependsOn: []string{"c"}},
				{ID: "b", DependsOn: []string{"a"}},
				{ID: "c", DependsOn: []string{"b"}},
			},
			want: "cycle",
		},
	} {
		if _, err := planFor(t, tc.items...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", name, err, tc.want)
		}
	}
}

func TestFleetPlanOrdersTransitiveDependents(t *testing.T) {
	plan, err := planFor(t,
		fleetTaskItem{ID: "research"},
		fleetTaskItem{ID: "backend", DependsOn: []string{"research"}},
		fleetTaskItem{ID: "frontend", DependsOn: []string{"research"}},
		fleetTaskItem{ID: "integration", DependsOn: []string{"backend", "frontend"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ordered(0, 3) {
		t.Error("integration transitively depends on research and must be ordered against it")
	}
	if plan.ordered(1, 2) {
		t.Error("backend and frontend share a dependency but not an order: they run in parallel")
	}
	if roots := plan.roots(); len(roots) != 1 || roots[0] != 0 {
		t.Errorf("roots = %v, want just research", roots)
	}
}

// The unlock: implement → review legitimately touch the same files, which a
// flat fleet could never express.
func TestFleetOrderedWritersMayShareWritePaths(t *testing.T) {
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := planFor(t,
		fleetTaskItem{ID: "implement"},
		fleetTaskItem{ID: "review", DependsOn: []string{"implement"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ordered.validateConcurrentWriteClaims([]WritePathSet{claim, claim}); err != nil {
		t.Fatalf("ordered writers must be allowed to share paths: %v", err)
	}

	concurrent, err := planFor(t, fleetTaskItem{ID: "a"}, fleetTaskItem{ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := concurrent.validateConcurrentWriteClaims([]WritePathSet{claim, claim}); err == nil {
		t.Fatal("two writers that can run at once must still fail preflight on overlap")
	}
}

func TestFleetConcurrentDirectoryClaimsPassPreflight(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := planFor(t, fleetTaskItem{ID: "a"}, fleetTaskItem{ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := concurrent.validateConcurrentWriteClaims([]WritePathSet{claim, claim}); err != nil {
		t.Fatalf("concurrent directory claims must pass preflight: %v", err)
	}
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := concurrent.validateConcurrentWriteClaims([]WritePathSet{whole, whole}); err != nil {
		t.Fatalf("omitted write_paths must queue in the scheduler, not fail preflight: %v", err)
	}
}

func TestFleetPlanSkipsWholeDownstreamBranch(t *testing.T) {
	plan, err := planFor(t,
		fleetTaskItem{ID: "research"},
		fleetTaskItem{ID: "implement", DependsOn: []string{"research"}},
		fleetTaskItem{ID: "review", DependsOn: []string{"implement"}},
		fleetTaskItem{ID: "unrelated"},
	)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]fleetItemResult, 4)
	for i := range results {
		results[i] = fleetItemResult{index: i, status: fleetItemPending}
	}
	results[0].status = fleetItemFailed
	plan.skipDependents(results, 0)

	if results[1].status != fleetItemSkipped || results[2].status != fleetItemSkipped {
		t.Fatalf("statuses = %q/%q, want the whole downstream branch skipped", results[1].status, results[2].status)
	}
	if !strings.Contains(results[2].err.Error(), `depends on "research"`) {
		t.Fatalf("skip reason = %v, want it to name the broken dependency", results[2].err)
	}
	if results[3].status != fleetItemPending {
		t.Error("an unrelated branch must not be skipped by another branch's failure")
	}
}

// End to end: a dependent never runs when its dependency failed.
func TestFleetSkipsDependentsOfFailedTask(t *testing.T) {
	root := t.TempDir()
	prov := &fleetScriptedFailureProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))
	fleet := NewFleetTool(task)
	ctx := withCallContext(context.Background(), "fleet-call", event.Discard, nil, false)

	out, _ := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"FAIL research","read_only":true},
		{"id":"implement","prompt":"implement","depends_on":["research"],"write_paths":["api"]},
		{"id":"sibling","prompt":"sibling","read_only":true}
	]}`))

	if !strings.Contains(out, "skipped") || !strings.Contains(out, `depends on "research"`) {
		t.Fatalf("aggregate must report the dependent as skipped with its reason:\n%s", out)
	}
	if prov.ran("implement") {
		t.Fatal("a dependent of a failed task must never start")
	}
	if !prov.ran("sibling") {
		t.Fatal("an independent branch must still run when another branch fails")
	}
}

type fleetScriptedFailureProvider struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (p *fleetScriptedFailureProvider) Name() string { return "fleet-failure" }

func (p *fleetScriptedFailureProvider) ran(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[name]
}

func (p *fleetScriptedFailureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	last := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			last = m.Content
		}
	}
	p.mu.Lock()
	if p.seen == nil {
		p.seen = map[string]bool{}
	}
	for _, name := range []string{"implement", "sibling"} {
		if strings.Contains(last, name) {
			p.seen[name] = true
		}
	}
	p.mu.Unlock()
	if strings.Contains(last, "FAIL") {
		return nil, errors.New("scripted provider failure")
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
