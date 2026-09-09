package agent

import "reasonix/internal/provider"

// outcomeRunState preserves explicit execution evidence before image enrichment.
// The fallback covers older and synthetic outcomes that have no image prepass.
func outcomeRunState(o toolOutcome) provider.ToolRunState {
	if o.runState != "" {
		return o.runState
	}
	if !o.executed {
		return provider.ToolRunNotStarted
	}
	return provider.ToolResultRunState(provider.Message{Content: o.output + "\n" + o.errMsg})
}
