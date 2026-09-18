package boot

import (
	"slices"
	"testing"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func withMCPResourceToolNames(names []string) []string {
	out := append(append([]string{}, names...), plugin.MCPResourceToolNames()...)
	slices.Sort(out)
	return out
}

func TestMCPResourceToolsJoinProviderSurfaceOnlyWhenConfigured(t *testing.T) {
	reg := tool.NewRegistry()
	host := plugin.NewHost()
	defer host.Close()
	addMCPResourceTools(reg, host, nil)
	if _, ok := reg.Get(plugin.ListMCPResourcesName); ok {
		t.Fatal("resource tools must stay unregistered without configured servers")
	}
	applyUnifiedProviderToolSurface(reg)
	if names := schemaNames(reg); len(names) != 0 {
		t.Fatalf("empty MCP config leaked tools: %v", names)
	}

	addMCPResourceTools(reg, host, []plugin.Spec{{Name: "docs"}})
	applyUnifiedProviderToolSurface(reg)
	got := schemaNames(reg)
	want := plugin.MCPResourceToolNames()
	if len(got) != len(want) {
		t.Fatalf("schemas = %v, want %v", got, want)
	}
	slices.Sort(got)
	wantSorted := append([]string(nil), want...)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Fatalf("schemas = %v, want %v", got, want)
	}
}

func schemaNames(reg *tool.Registry) []string {
	schemas := reg.Schemas()
	out := make([]string, len(schemas))
	for i, schema := range schemas {
		out[i] = schema.Name
	}
	return out
}
