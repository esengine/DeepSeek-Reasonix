// usecapability_batching.go — whether a proxied call may share a batch.
package usecap

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/contract/tool"
)

// Sequential answers for whatever this call proxies rather than for the proxy.
// Reading the directory can share a batch; an inspect may start a server and a
// decline writes the ledger, so those keep their place in provider order.
func (t *UseCapabilityTool) Sequential(ctx context.Context, args json.RawMessage) bool {
	var p struct {
		Action       string          `json:"action"`
		CapabilityID string          `json:"capability_id"`
		Arguments    json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "list":
		return false
	case "call":
		return t.proxiedSequential(ctx, strings.TrimSpace(p.CapabilityID), p.Arguments)
	default:
		return true
	}
}

// proxiedSequential reads the target's own contract out of the catalog the call
// named it by. An id the catalog cannot resolve keeps its own place: an unknown
// target is exactly the one whose effects cannot be assumed.
func (t *UseCapabilityTool) proxiedSequential(ctx context.Context, id string, inner json.RawMessage) bool {
	e, ok := t.currentCatalog().Lookup(id)
	if !ok || !e.ReadOnly {
		return true
	}
	if e.ToolName == "" || t.registry == nil {
		return false
	}
	target, ok := t.registry.Get(e.ToolName)
	if !ok {
		return true
	}
	return tool.RunsSequentially(ctx, target, inner)
}
