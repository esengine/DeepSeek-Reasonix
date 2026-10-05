package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/usecap"
	"slices"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

type namedSchemaTool struct {
	name   string
	schema string
}

func (s namedSchemaTool) Name() string            { return s.name }
func (s namedSchemaTool) Description() string     { return s.name + " for the arguments gate" }
func (s namedSchemaTool) ReadOnly() bool          { return false }
func (s namedSchemaTool) Schema() json.RawMessage { return json.RawMessage(s.schema) }
func (s namedSchemaTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func argumentCatalog(t *testing.T) *usecap.UseCapabilityTool {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(namedSchemaTool{
		name: "remember",
		schema: `{"type":"object","properties":{
			"description":{"type":"string"},"body":{"type":"string"},
			"name":{"type":"string"},"scope":{"type":"string"}},
			"required":["description","body"]}`,
	})
	reg.SetProviderVisibleTools([]string{"use_capability"})
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	return usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)
}

type listedCapability struct {
	ID       string   `json:"id"`
	Requires []string `json:"requires"`
	Accepts  []string `json:"accepts"`
}

func listedCapabilities(t *testing.T, uc *usecap.UseCapabilityTool) []listedCapability {
	t.Helper()
	out, err := uc.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var payload struct {
		Capabilities []listedCapability `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("list payload: %v\n%s", err, out)
	}
	return payload.Capabilities
}

func TestListNamesTheArgumentsACallWillBeHeldTo(t *testing.T) {
	uc := argumentCatalog(t)
	for _, c := range listedCapabilities(t, uc) {
		if c.ID != "tool:remember" {
			continue
		}
		if !slices.Equal(c.Requires, []string{"description", "body"}) {
			t.Fatalf("requires = %v, want [description body]", c.Requires)
		}
		if !slices.Equal(c.Accepts, []string{"body", "description", "name", "scope"}) {
			t.Fatalf("accepts = %v, want every declared property, sorted", c.Accepts)
		}
		return
	}
	t.Fatal("tool:remember missing from the catalog listing")
}

func TestListOmitsArgumentsItCannotRead(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(namedSchemaTool{name: "opaque", schema: `{"type":"object"}`})
	reg.SetProviderVisibleTools([]string{"use_capability"})
	catalog := func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	}
	uc := usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, catalog)
	for _, c := range listedCapabilities(t, uc) {
		if c.ID == "tool:opaque" && (len(c.Requires) > 0 || len(c.Accepts) > 0) {
			t.Fatalf("a propertyless schema reported arguments: requires=%v accepts=%v", c.Requires, c.Accepts)
		}
	}
}
