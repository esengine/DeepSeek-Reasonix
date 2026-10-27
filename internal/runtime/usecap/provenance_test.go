package usecap

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

func TestCatalogAnswersCarryTheExternalLabelOnlyWhenAnMCPDescriptionIsInThem(t *testing.T) {
	withMCP := capability.Catalog{Entries: []capability.Entry{
		{ID: "mcp-tool:ops/deploy", Kind: capability.KindMCPTool, Name: "deploy", Source: "ops", Description: "server-written text"},
		{ID: "skill:review", Kind: capability.KindSkill, Name: "review", Description: "mine"},
	}}
	skillsOnly := capability.Catalog{Entries: []capability.Entry{{ID: "skill:review", Kind: capability.KindSkill, Name: "review"}}}
	for _, c := range []struct {
		name string
		cat  capability.Catalog
		args string
		want tool.ProvenanceKind
	}{
		{"list with an MCP tool", withMCP, `{"action":"list"}`, tool.ProvenanceMCP},
		{"search with an MCP tool", withMCP, `{"action":"search","query":"deploy"}`, tool.ProvenanceMCP},
		{"inspect an MCP tool", withMCP, `{"action":"inspect","capability_id":"mcp-tool:ops/deploy"}`, tool.ProvenanceMCP},
		{"inspect a skill", withMCP, `{"action":"inspect","capability_id":"skill:review"}`, ""},
		{"list with skills only", skillsOnly, `{"action":"list"}`, ""},
	} {
		proxy := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog { return c.cat })
		if got := tool.ProvenanceOf(proxy, json.RawMessage(c.args)).Kind; got != tool.ProvenanceKind(c.want) && !(c.want == "" && got == tool.ProvenanceWorkspace) {
			t.Errorf("%s: provenance %q, want %q", c.name, got, c.want)
		}
	}
}
