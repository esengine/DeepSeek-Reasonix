package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestServerStatusPreservesCanonicalDiagnosticBindings(t *testing.T) {
	c := &Client{name: "figma", spec: Spec{Package: "design", StripRawPrefix: "figma_"}}
	adapter := &remoteTool{client: c, name: ModelToolName("figma", "search"), rawName: "figma_search", visibleName: "search"}
	c.toolCatalog = toolCatalogSnapshot{listed: true, adapters: []tool.Tool{adapter}}
	h := NewHost()
	h.clients = []*Client{c}
	statuses := h.Servers()
	if len(statuses) != 1 || len(statuses[0].ToolBindings) != 1 {
		t.Fatalf("bindings missing: %+v", statuses)
	}
	binding := statuses[0].ToolBindings[0]
	if binding.Package != "design" || binding.RawName != "figma_search" || binding.VisibleName != "search" || binding.CallableName != "mcp__figma__search" {
		t.Fatalf("lost runtime identity: %+v", binding)
	}
	statuses[0].ToolBindings[0].RawName = "changed"
	if h.Servers()[0].ToolBindings[0].RawName != "figma_search" {
		t.Fatal("snapshot aliases host state")
	}
	raw, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ToolBindings") {
		t.Fatal("diagnostic metadata leaked into serialized status")
	}
}
