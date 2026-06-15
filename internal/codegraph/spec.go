package codegraph

import "reasonix/internal/plugin"

// MCPSpec returns the built-in CodeGraph MCP plugin spec used by every Reasonix
// launch path. Keeping this in one place prevents lifecycle env overrides from
// drifting between boot, token-economy on-demand enablement, and manual MCP
// reconnects.
func MCPSpec(bin, root string) plugin.Spec {
	return plugin.Spec{
		Name:           "codegraph",
		StripRawPrefix: "codegraph_",
		Command:        bin,
		Args:           []string{"serve", "--mcp"},
		Env: map[string]string{
			DaemonIdleTimeoutEnv: ReasonixDaemonIdleTimeoutMS,
		},
		Dir:               root,
		ReadOnlyToolNames: ReadOnlyToolNames(),
		// The daemon walks and indexes the whole tree; below-normal priority keeps
		// it from starving the user's machine (#3797).
		LowPriority: true,
	}
}
