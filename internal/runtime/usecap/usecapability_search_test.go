package usecap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

type searchableCapabilityTool struct{ name, description string }

func (t searchableCapabilityTool) Name() string          { return t.name }
func (t searchableCapabilityTool) Description() string   { return t.description }
func (searchableCapabilityTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (searchableCapabilityTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}
func (searchableCapabilityTool) ReadOnly() bool { return true }

func TestUseCapabilitySearchFindsToolBeyondLargeCatalog(t *testing.T) {
	registry := tool.NewRegistry()
	target := searchableCapabilityTool{name: "web_fetch", description: "Fetch a URL over HTTPS or HTTP and return its text content."}
	registry.Add(target)
	entries := make([]capability.Entry, 0, 1001)
	for i := range 1000 {
		entries = append(entries, capability.Entry{
			ID: fmt.Sprintf("mcp-tool:graphics/tool-%04d", i), Kind: capability.KindMCPTool,
			Name: fmt.Sprintf("graphics/tool-%04d", i), Description: "Edit a scene asset", Status: capability.StatusReady,
		})
	}
	entries = append(entries, capability.Entry{
		ID: "tool:web_fetch", Kind: capability.KindTool, Name: target.name, ToolName: target.name,
		Description: target.description, Status: capability.StatusReady, ReadOnly: true,
	})
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, registry, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: entries}
	})

	out, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"fetch URL over HTTP","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"id": "tool:web_fetch"`) || !strings.Contains(out, `"input_schema"`) {
		t.Fatalf("search did not load the matching callable definition:\n%s", out)
	}
	if strings.Contains(out, "graphics/tool-") {
		t.Fatalf("search returned unrelated catalog entries:\n%s", out)
	}
}

func TestUseCapabilitySearchRequiresQueryAndBoundsLimit(t *testing.T) {
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog {
		return capability.Catalog{}
	})
	if _, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"search"}`)); err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("missing query error = %v", err)
	}
}

func TestRestrictedCapabilitySearchDoesNotLeakOutsideAllowlist(t *testing.T) {
	raw := `{"query":"data","matches":[{"id":"mcp-tool:allowed/read","kind":"mcp-tool","name":"allowed/read","status":"ready"},{"id":"mcp-tool:secret/read","kind":"mcp-tool","name":"secret/read","status":"ready"}],"note":"full"}`
	out := FilterCapabilitySearchResult(raw, map[string]bool{"mcp-tool:allowed/read": true})
	if !strings.Contains(out, "mcp-tool:allowed/read") || strings.Contains(out, "mcp-tool:secret/read") {
		t.Fatalf("restricted search leaked or removed the wrong capability:\n%s", out)
	}
}

func TestCapabilityListLeadsWithSearchGuidanceBeforeLargeInventory(t *testing.T) {
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "tool:late", Kind: capability.KindTool, Name: "late", ToolName: "late", Status: capability.StatusReady,
		}}}
	})
	out, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	note, inventory := strings.Index(out, `"note"`), strings.Index(out, `"capabilities"`)
	if note < 0 || inventory < 0 || note > inventory || !strings.Contains(out[:inventory], "action=search") {
		t.Fatalf("list preview does not lead with bounded-search guidance:\n%s", out)
	}
}
