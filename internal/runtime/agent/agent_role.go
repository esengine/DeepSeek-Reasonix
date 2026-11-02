// agent_role.go — what an agent is, fixed for its life.
package agent

// agentRole is decided when the agent is built and does not change afterwards:
// a planner that may not execute or an executor told to reach for its tools.
// That fixed lifetime separates these from planMode and the language settings
// beside them, which the user moves between turns.
type agentRole struct {
	// executorHandoff is set by Coordinator for the executor agent alone.
	executorHandoff bool
	// readOnlyExecution is a construction-time defense for planner/research
	// agents. Unlike planMode it is not a collaboration toggle: it stays on for
	// the agent's lifetime and validates proxy calls after resolution.
	readOnlyExecution bool
	// plannerMCPExecution lets the two-model Planner alone run authorized,
	// non-destructive MCP targets without readOnlyHint. Writers, bash and
	// destructive MCP stay blocked, and read-only sub-agents leave it false.
	plannerMCPExecution bool
}

func newAgentRole(opts Options) agentRole {
	return agentRole{
		readOnlyExecution:   opts.ReadOnlyExecution,
		plannerMCPExecution: opts.PlannerMCPExecution,
	}
}
