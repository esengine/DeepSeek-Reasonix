package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/usecap"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

// delegationTools are the dispatchers hidden from the provider schema. A model
// reaches them only through use_capability, so their capability ids are the
// whole discovery surface.
func delegationTools() []tool.Tool {
	task := &TaskTool{}
	return []tool.Tool{task, NewReadOnlyTaskTool(task), NewParallelTasksTool(task, nil), NewFleetTool(task)}
}

func aliasCatalog(t *testing.T) (*tool.Registry, *usecap.UseCapabilityTool) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tl := range delegationTools() {
		reg.Add(tl)
	}
	reg.SetProviderVisibleTools([]string{"use_capability"})
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	return reg, usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)
}

// TestDeclaredAliasIsListedInspectableAndCallable is the gate this package was
// missing: `task:subagent` was named in the proxy description and accepted by
// call, while list and inspect denied it existed. A model that checks before
// dispatching concluded the harness had no subagents at all and did the work
// inline — the delegation was lost silently, with nothing failing.
func TestDeclaredAliasIsListedInspectableAndCallable(t *testing.T) {
	reg, uc := aliasCatalog(t)
	ctx := context.Background()

	listed, err := uc.Execute(ctx, json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, tl := range delegationTools() {
		aliased, ok := tl.(tool.CapabilityAliased)
		if !ok {
			t.Fatalf("%s declares no capability alias, so nothing names it in the catalog", tl.Name())
		}
		for _, alias := range aliased.CapabilityAliases() {
			if !strings.Contains(listed, alias) {
				t.Errorf("action=list never names %q, so the model cannot discover it:\n%s", alias, listed)
			}
			out, err := uc.Execute(ctx, json.RawMessage(`{"action":"inspect","capability_id":"`+alias+`"}`))
			if err != nil {
				t.Errorf("inspect %q: %v", alias, err)
				continue
			}
			if !strings.Contains(out, `"input_schema"`) {
				t.Errorf("inspect %q returned no input schema, leaving the model to guess arguments:\n%s", alias, out)
			}
			rc, err := uc.ResolveCall(ctx, json.RawMessage(`{"action":"call","capability_id":"`+alias+`","arguments":{}}`))
			if err != nil {
				t.Errorf("call %q: %v", alias, err)
				continue
			}
			if rc.TargetName != tl.Name() {
				t.Errorf("call %q resolved to %q, want %q", alias, rc.TargetName, tl.Name())
			}
		}
	}
	_ = reg
}

// TestAliasesAreUnique keeps one id from naming two capabilities: Lookup
// returns the first match, so a collision would silently reroute a dispatch.
func TestAliasesAreUnique(t *testing.T) {
	reg, _ := aliasCatalog(t)
	cat := capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	owner := map[string]string{}
	for _, e := range cat.Entries {
		owner[e.ID] = e.ID
	}
	for _, e := range cat.Entries {
		for _, alias := range e.Aliases {
			if prev, ok := owner[alias]; ok {
				t.Errorf("alias %q on %s is already taken by %s", alias, e.ID, prev)
				continue
			}
			owner[alias] = e.ID
		}
	}
}

// TestUnknownAliasStillFails guards the other direction: accepting any id in a
// known namespace would turn a typo into a call on whatever tool shares the name.
func TestUnknownAliasStillFails(t *testing.T) {
	_, uc := aliasCatalog(t)
	for _, id := range []string{"task:nonexistent", "task:", "workflow:task", "web:task"} {
		if _, err := uc.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"`+id+`","arguments":{}}`)); err == nil {
			t.Errorf("%q resolved, but no catalog entry answers to it", id)
		}
	}
}
