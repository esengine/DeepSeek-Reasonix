package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/usecap"
	"testing"
)

import ()

// TestNonDelegatingCallHasNoProfile keeps the mark meaningful: the frontend
// reads a profile's presence as "this work left the context", so an ordinary
// tool must not carry one.
func TestNonDelegatingCallHasNoProfile(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(probeInertTool{})
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	uc := usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)
	rc, err := uc.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"tool:inert","arguments":{}}`))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := delegationProfile(rc.Target, rc.Args); got != nil {
		t.Errorf("an ordinary tool reported a delegation: %+v", got)
	}
	if got := delegationProfile(nil, nil); got != nil {
		t.Errorf("an unresolved target reported a delegation: %+v", got)
	}
}
