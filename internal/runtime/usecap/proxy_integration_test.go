package usecap_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/usecap"
	"strings"
	"testing"
)

// The delegation calls that motivated this: a fleet item was refused with
// `json: unknown field "name"`, which names neither the level the field sits on
// nor what that level accepts. One observed run tried "name", then "title",
// then "description", paying a round trip for each before finding "prompt".
func TestContractHintDescendsIntoArrayItems(t *testing.T) {
	schema := (&delegation.FleetTool{}).Schema()
	got := usecap.ContractHint(schema, json.RawMessage(`{"tasks":[{"name":"fix-alpha","prompt":"do it"}]}`))
	// Membership, not position: the accepted list is alphabetical, so anchoring
	// on whichever key sorts first breaks every time one is added.
	for _, want := range []string{`"name" is not a parameter of a ` + "`tasks`" + ` item`, ` accepts `, `"depends_on"`, `"prompt"`} {
		if !strings.Contains(got, want) {
			t.Errorf("hint = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "this capability") {
		t.Errorf("hint = %q, want the item level named, not the outer call", got)
	}
}

// Well-formed items stay silent: descending must not invent a contract breach
// where the call matches the schema at every level.
func TestContractHintSilentOnWellFormedItems(t *testing.T) {
	schema := (&delegation.FleetTool{}).Schema()
	if got := usecap.ContractHint(schema, json.RawMessage(`{"tasks":[{"prompt":"a"},{"prompt":"b","model":"m"}]}`)); got != "" {
		t.Errorf("well-formed items produced hint %q, want silence", got)
	}
}

// The end of the path the hint exists for: a capability call whose target
// refused the arguments comes back carrying the level the field belongs to.
func TestCapabilityCallFailureCarriesTheItemContract(t *testing.T) {
	uc := &usecap.UseCapabilityTool{}
	resolved := tool.ResolvedCall{
		Target: &delegation.FleetTool{},
		Args:   json.RawMessage(`{"tasks":[{"name":"fix-alpha","prompt":"do it"}]}`),
	}
	err := usecap.RecordCallFailure(uc, resolved, errors.New(`invalid args: json: unknown field "name"`))
	if err == nil || !strings.Contains(err.Error(), "`tasks`"+" item") {
		t.Fatalf("err = %v, want the item level named alongside the target's own message", err)
	}
}

// The catalog reached 41 KB and spilled past the 32 KiB cap: a model that asked
// what it had was handed a pointer to a file. Descriptions were two thirds of it.
func TestTheCatalogFitsInContext(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "capability_list.json"))
	if err != nil {
		t.Skipf("no recorded catalog to measure: %v", err)
	}
	var doc struct {
		Capabilities []struct {
			Description string `json:"description"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	before, after := 0, 0
	for _, c := range doc.Capabilities {
		before += len(c.Description)
		after += len(usecap.CapabilityLead(c.Description))
	}
	t.Logf("%d entries: description bytes %d -> %d", len(doc.Capabilities), before, after)
	if after >= before/2 {
		t.Fatalf("descriptions %d -> %d; the listing still carries most of the text", before, after)
	}
	if total := len(raw) - before + after; total > agent.MaxToolOutputBytes {
		t.Fatalf("listing would still be %d bytes, over the %d cap that spills it", total, agent.MaxToolOutputBytes)
	}
}

func TestPlannerToolRegistryClonesUseCapability(t *testing.T) {
	parent := tool.NewRegistry()
	ledger := capability.NewLedger()
	proxy := usecap.NewUseCapabilityTool(context.Background(), nil, nil, parent, ledger, nil, nil)
	parent.Add(proxy)
	parent.Add(fakeRegistryTool{name: "read_file", readOnly: true})

	planner := agent.PlannerToolRegistry(parent)
	got, ok := planner.Get("use_capability")
	if !ok {
		t.Fatal("planner missing use_capability")
	}
	uc, ok := got.(*usecap.UseCapabilityTool)
	if !ok {
		t.Fatalf("planner proxy type = %T, want *UseCapabilityTool", got)
	}
	if uc == proxy {
		t.Fatal("planner must not share the executor UseCapabilityTool pointer")
	}
	if usecap.LedgerOf(uc) == ledger {
		t.Fatal("planner frontend must not share the executor capability ledger")
	}
}

type fakeRegistryTool struct {
	name     string
	readOnly bool
}

func (t fakeRegistryTool) Name() string                                             { return t.name }
func (t fakeRegistryTool) Description() string                                      { return t.name }
func (t fakeRegistryTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (t fakeRegistryTool) ReadOnly() bool                                           { return t.readOnly }
func (t fakeRegistryTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
