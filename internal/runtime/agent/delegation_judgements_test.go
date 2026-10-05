package agent_test

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
	"testing"
)

// Delegation spawns work rather than changing state, so a failed one does not
// open the dependency barrier — batchCallIsMutatingFailure exempts it. Being
// skipped BY the barrier is the same question, and a bare !ReadOnly answers it
// the other way, dropping every delegation left in the batch.
func TestBatchDoesNotSkipDelegationAsADependentModification(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&delegation.TaskTool{})

	for _, name := range []string{"task"} {
		call := provider.ToolCall{Name: name, Arguments: `{"prompt":"do a thing","description":"d"}`}
		if agent.BatchCallStaticallySkippable(reg, call) {
			t.Errorf("%s is skippable as a dependent modification, but a failed %s does not open the barrier either",
				name, name)
		}
	}
}

// TestDelegationSurvivesTheProxyBoundary is the gate the frontend depended on
// and never had. The model only ever calls use_capability, so a dispatch that
// loses its profile there reaches the UI anonymous: the panel counted no
// sub-agents at all, and the card fell back to reading a delegate's step count
// as a number of delegates.
func TestDelegationSurvivesTheProxyBoundary(t *testing.T) {
	reg := tool.NewRegistry()
	task := &delegation.TaskTool{}
	reg.Add(task)
	reg.Add(delegation.NewFleetTool(task))
	reg.Add(delegation.NewReadOnlyTaskTool(task))
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	uc := usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)

	for _, tc := range []struct {
		id, args  string
		wantName  string
		wantCount int
	}{
		{`task:subagent`, `{"prompt":"x"}`, "task", 1},
		{`task:subagent`, `{"prompt":"x","profile":"implementer"}`, "implementer", 1},
		{`task:fleet`, `{"tasks":[{"prompt":"a"},{"prompt":"b"},{"prompt":"c"}]}`, "fleet", 3},
	} {
		call := `{"action":"call","capability_id":"` + tc.id + `","arguments":` + tc.args + `}`
		rc, err := uc.ResolveCall(context.Background(), json.RawMessage(call))
		if err != nil {
			t.Fatalf("resolve %s: %v", tc.id, err)
		}
		got := agent.DelegationProfile(rc.Target, rc.Args)
		if got == nil {
			t.Errorf("%s %s resolved to a dispatch with no profile — the UI cannot name or count it", tc.id, tc.args)
			continue
		}
		if got.Name != tc.wantName || got.Count != tc.wantCount {
			t.Errorf("%s %s reported %q×%d, want %q×%d", tc.id, tc.args, got.Name, got.Count, tc.wantName, tc.wantCount)
		}
	}
}

// Planning keeps the delegation it was designed around. read_only_task exists
// so a plan can research in an isolated context; a barrier reading "delegation"
// as "side effect" would take that away, and one reading "no mutation receipt"
// as "safe" lets the writer-capable ones through — the hole this closes.
func TestPlanningPhaseSplitsDelegationByWriterCapability(t *testing.T) {
	task := &delegation.TaskTool{}
	reg := tool.NewRegistry()
	a := agent.New(nil, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	a.SetPlanMode(true)

	blocked := map[string]bool{}
	for _, tl := range []tool.Tool{
		task,
		delegation.NewReadOnlyTaskTool(task),
		delegation.NewFleetTool(task),
		delegation.NewParallelTasksTool(task, reg),
		delegation.NewSubagentResultTool(task),
		delegation.NewSubagentListTool(task),
	} {
		safety := planmode.PlanSafetyUnknown
		if c, ok := tl.(tool.PlanModeClassifier); ok {
			safety = planmode.PlanSafetyUnsafe
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			}
		}
		got := agent.PlanModeDecision(a, tl, safety)
		blocked[tl.Name()] = got.Blocked
		// The verdict has to follow what the tool declares about itself, or the
		// barrier has started classifying delegation by name.
		want := !tl.ReadOnly() && safety != planmode.PlanSafetySafe
		if got.Blocked != want {
			t.Errorf("%q readOnly=%v safety=%v blocked=%v, want %v", tl.Name(), tl.ReadOnly(), safety, got.Blocked, want)
		}
	}

	for name, wantBlocked := range map[string]bool{
		"read_only_task":       false,
		"parallel_tasks":       false,
		"read_subagent_result": false,
		"list_subagents":       false,
		"task":                 true,
	} {
		got, ok := blocked[name]
		if !ok {
			t.Errorf("%q was not registered; the anchor no longer measures anything", name)
			continue
		}
		if got != wantBlocked {
			t.Errorf("%q blocked=%v during planning, want %v", name, got, wantBlocked)
		}
	}
}
