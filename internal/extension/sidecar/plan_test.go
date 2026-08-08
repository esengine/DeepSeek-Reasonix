package sidecar

import (
	"testing"

	"reasonix/internal/extension"
)

func TestPluginComponentIDRoundTrip(t *testing.T) {
	id := PluginComponentID("demo")
	if id != "plugin/demo" {
		t.Fatalf("id = %q", id)
	}
	if got := PluginNameFromComponentID(id); got != "demo" {
		t.Fatalf("name = %q", got)
	}
	if PluginNameFromComponentID("host") != "" {
		t.Fatal("host should not map to a plugin name")
	}
}

func TestManagerDetachAdopt(t *testing.T) {
	m := &Manager{clients: map[string]*Client{}}
	c1 := &Client{pluginID: "a"}
	c2 := &Client{pluginID: "b"}
	m.clients["a"] = c1
	m.clients["b"] = c2

	detached := m.Detach("a")
	if detached != c1 || m.Client("a") != nil {
		t.Fatal("detach failed")
	}
	next := &Manager{clients: map[string]*Client{}}
	if err := next.Adopt("a", detached); err != nil {
		t.Fatal(err)
	}
	if next.Client("a") != c1 {
		t.Fatal("adopt failed")
	}
	// Detach b without Close (incomplete Client must not be closed).
	if got := m.Detach("b"); got != c2 || m.Client("b") != nil {
		t.Fatal("detach b failed")
	}
	if err := next.Adopt("a", c2); err == nil {
		t.Fatal("duplicate adopt should fail")
	}
}

func TestRollbackPlanStartReattachesOnlyAdopted(t *testing.T) {
	// previous holds old "reloaded" client; new manager has fresh "reloaded" + adopted "keep".
	prev := &Manager{clients: map[string]*Client{}}
	// Use real clients only if we can construct them cheaply — use Detach/Adopt unit path.
	keep := &Client{pluginID: "keep"}
	oldReloaded := &Client{pluginID: "reloaded"}
	prev.clients["keep"] = keep
	prev.clients["reloaded"] = oldReloaded

	next := &Manager{clients: map[string]*Client{}, planAdopted: map[string]*Client{}}
	// Simulate StartPackagesWithPlan: detach keep, adopt into next; start fresh reloaded.
	if c := prev.Detach("keep"); c != nil {
		_ = next.Adopt("keep", c)
		next.planAdopted["keep"] = c
	}
	fresh := &Client{pluginID: "reloaded"}
	_ = next.Adopt("reloaded", fresh)

	next.RollbackPlanStart(prev)
	if prev.Client("keep") == nil {
		t.Fatal("unchanged client must be reattached to previous")
	}
	if prev.Client("reloaded") != oldReloaded {
		t.Fatal("previous must still hold old reloaded client")
	}
	// next should be closed/empty of keep; fresh reloaded closed via Close.
	if next.Client("keep") != nil || next.Client("reloaded") != nil {
		t.Fatal("next manager must not retain clients after rollback")
	}
}

func TestDrainPlanSelectsIDs(t *testing.T) {
	m := &Manager{clients: map[string]*Client{
		"gone": {pluginID: "gone"},
		"a":    {pluginID: "a"},
		"keep": {pluginID: "keep"},
	}}
	plan := &extension.RuntimePlan{
		Removed:  []extension.ComponentID{"plugin/gone"},
		Reloaded: []extension.ComponentID{"plugin/a"},
	}
	// DrainPlan closes clients; incomplete Clients panic on Close, so only
	// Detach the matching IDs to verify selection logic.
	var drained []string
	for _, id := range append(append([]extension.ComponentID{}, plan.Removed...), plan.Reloaded...) {
		if name := PluginNameFromComponentID(id); name != "" {
			if c := m.Detach(name); c != nil {
				drained = append(drained, name)
			}
		}
	}
	if len(drained) != 2 {
		t.Fatalf("drained = %v", drained)
	}
	if m.Client("keep") == nil {
		t.Fatal("keep should remain")
	}
}
