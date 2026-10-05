package delegation

import (
	"reasonix/internal/runtime/usecap"
	"strings"
	"testing"
)

// The provider schema hides the dispatchers, so this description and an explicit
// action=list are the model's whole discovery surface for them. It named only
// task:subagent, and across 21 observed sessions the batch dispatchers were
// reached 6 times against 74 single delegations: a capability nothing names is
// one the model has no reason to look for.
func TestTheProxyDescriptionNamesAWayToDelegateMoreThanOneThing(t *testing.T) {
	desc := (*usecap.UseCapabilityTool)(nil).Description()
	batch := (*FleetTool)(nil).CapabilityAliases()
	if len(batch) == 0 {
		t.Fatal("the fleet dispatcher declares no capability id")
	}
	if !strings.Contains(desc, batch[0]) {
		t.Fatalf("the proxy description never names %q, so nothing points the model at it:\n%s", batch[0], desc)
	}
	// Naming an id the catalog cannot resolve is the opposite failure, and the
	// boot effect test covers it — but only for ids listed here.
	found := false
	for _, id := range usecap.CapabilityIDExamples {
		if id == batch[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q is named in the description but not in CapabilityIDExamples, so nothing checks it resolves", batch[0])
	}
}
