package usecap

import (
	"strings"

	"reasonix/internal/contract/tool"
)

// resolveDisabled turns a configuration-disabled MCP target into the same typed
// refusal direct calls receive, so child and use_capability paths carry the
// disabled-tool identity.
func (t *UseCapabilityTool) resolveDisabled(base tool.ResolvedCall, id, name string) (tool.ResolvedCall, bool) {
	if t.registry == nil {
		return base, false
	}
	candidates := []string{strings.TrimSpace(name), strings.TrimSpace(id)}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		refusal, ok := t.registry.MCPRefusal(candidate)
		if !ok {
			continue
		}
		base.Unavailable = true
		base.SkipExecute = true
		base.UnavailableReason = refusal.Message
		base.RefusalCode = refusal.Code
		base.Result = refusal.String()
		base.TargetName = candidate
		base.ReadOnly = false
		base.Commit = func() error {
			if t.ledger != nil {
				t.ledger.MarkUnavailable(id, refusal.Message)
			}
			if t.audit != nil {
				t.audit.RecordMCPProxy(false, true, true)
			}
			return nil
		}
		return base, true
	}
	return base, false
}

// resolveUnavailable fills the host-proven unavailable shape shared by the
// side-effect-free resolution failures (missing config, unknown tool).
func (t *UseCapabilityTool) resolveUnavailable(base tool.ResolvedCall, id, modelName, reason string) tool.ResolvedCall {
	base.Unavailable = true
	base.UnavailableReason = reason
	base.SkipExecute = true
	base.Result = "capability unavailable: " + reason
	base.TargetName = modelName
	base.ReadOnly = false
	base.Commit = func() error {
		if t.ledger != nil {
			t.ledger.MarkUnavailable(id, reason)
		}
		if t.audit != nil {
			t.audit.RecordMCPProxy(false, true, true)
		}
		return nil
	}
	return base
}
