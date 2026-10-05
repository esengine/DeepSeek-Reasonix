package boot

// Effect test for delegation identity: what a frontend can actually tell about
// a dispatched sub-agent when the model reached it through use_capability.

import (
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
)

// TestEffectProxiedDelegationArrivesNamed pins the identity at its final
// boundary. Every dispatcher is hidden behind use_capability, so the tool name
// on the wire is the proxy's. Without a profile on that dispatch a reader
// cannot say who ran or how many ran — which is how one delegate's 64 steps
// came to be displayed as 64 delegates.
func TestEffectProxiedDelegationArrivesNamed(t *testing.T) {
	var mu sync.Mutex
	var dispatches []event.Tool
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.ToolDispatch || e.Tool.Name != "use_capability" {
			return
		}
		mu.Lock()
		dispatches = append(dispatches, e.Tool)
		mu.Unlock()
	})

	probe := &capabilityProbeProvider{calls: []string{
		`{"action":"call","capability_id":"task:subagent","arguments":{"description":"probe","prompt":"reply done and stop"}}`,
	}}
	runProbeWith(t, "boot-delegation-identity", probe, sink)

	mu.Lock()
	defer mu.Unlock()
	var named *event.Profile
	for _, d := range dispatches {
		if d.Profile != nil {
			named = d.Profile
			break
		}
	}
	if named == nil {
		var seen []string
		for _, d := range dispatches {
			seen = append(seen, d.Name+"/"+d.ResolvedName)
		}
		t.Fatalf("a dispatched sub-agent reached the sink anonymous; dispatches: %s", strings.Join(seen, ", "))
	}
	if named.Name == "" {
		t.Errorf("delegation carries no name, so no reader can attribute the step: %+v", named)
	}
	if named.Count < 1 {
		t.Errorf("delegation reports %d sub-agents, so a panel would under-count it", named.Count)
	}
}
