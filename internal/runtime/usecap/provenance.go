package usecap

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

// Provenance labels a catalog answer that quotes text an MCP server wrote about
// its own tools. A call is not labelled here: its target answers for itself.
func (t *UseCapabilityTool) Provenance(args json.RawMessage) tool.Provenance {
	var p struct {
		Action       string `json:"action"`
		CapabilityID string `json:"capability_id"`
	}
	if json.Unmarshal(args, &p) != nil {
		return tool.Provenance{}
	}
	cat := t.currentCatalog()
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case ActionList, ActionSearch:
		if slices.ContainsFunc(cat.Entries, isMCPEntry) {
			return tool.Provenance{Kind: tool.ProvenanceMCP}
		}
	case ActionInspect:
		if e, ok := cat.Lookup(strings.TrimSpace(p.CapabilityID)); ok && isMCPEntry(e) {
			return tool.Provenance{Kind: tool.ProvenanceMCP, Source: e.Source}
		}
	}
	return tool.Provenance{}
}

func isMCPEntry(e capability.Entry) bool {
	return e.Kind == capability.KindMCPTool || e.Kind == capability.KindMCPServer
}
