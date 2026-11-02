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
// open the dependency barrier. Being skipped BY the barrier is the same
// question, and a bare !ReadOnly answers it the other way.
func TestBatchDoesNotSkipDelegationAsADependentModification(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(delegation.NewSpawner(nil))

	call := provider.ToolCall{Name: delegation.SpawnToolName, Arguments: `{"tasks":[{"title":"t","task":"do a thing"}]}`}
	if agent.BatchCallStaticallySkippable(reg, call) {
		t.Errorf("%s is skippable as a dependent modification, but a failed one does not open the barrier either", call.Name)
	}
}

// The model only ever calls use_capability, so a dispatch that loses its
// profile there reaches the UI anonymous: the panel counts no sub-agents at all.
func TestDelegationSurvivesTheProxyBoundary(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(delegation.NewSpawner(nil))
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	uc := usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)

	for _, tc := range []struct {
		id, args  string
		wantName  string
		wantCount int
	}{
		{`task:subagent`, `{"tasks":[{"title":"a","task":"x"}]}`, delegation.SpawnToolName, 1},
		{`task:subagent`, `{"tasks":[{"title":"a","task":"x"},{"title":"b","task":"y"},{"title":"c","task":"z"}]}`, delegation.SpawnToolName, 3},
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

// Planning keeps the delegation it was designed around: sub-agents are
// read-only, so a plan can research in an isolated context.
func TestPlanningPhaseKeepsReadOnlyDelegation(t *testing.T) {
	reg := tool.NewRegistry()
	a := agent.New(nil, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	a.SetPlanMode(true)

	for _, tl := range []tool.Tool{
		delegation.NewSpawner(nil),
		delegation.NewSubagentResultTool(nil, ""),
	} {
		safety := planmode.PlanSafetyUnknown
		if c, ok := tl.(tool.PlanModeClassifier); ok {
			safety = planmode.PlanSafetyUnsafe
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			}
		}
		if got := agent.PlanModeDecision(a, tl, safety); got.Blocked {
			t.Errorf("%q blocked during planning, want allowed", tl.Name())
		}
	}
}
