package hook

// claudeAgentSpawningTools are every tool that spawns a subagent, and so maps to
// Claude's one "Agent" tool: the task delegators and each wrapper in
// skill.BuiltinSubagentTools. A hook matching "Agent" must see all of them or it
// misses whichever entry point is left out.
var claudeAgentSpawningTools = []string{
	"task", "read_only_task", "parallel_tasks",
	"explore", "locate", "research", "review", "security_review",
}

// claudeAgentDefaultDescriptions fill Claude Agent's required description
// field when the corresponding Reasonix tool does not expose one or the model
// omitted Reasonix's optional description. These are stable operation labels;
// the complete task remains in prompt for hook policy decisions.
var claudeAgentDefaultDescriptions = map[string]string{
	"task":            "Run delegated subagent task",
	"read_only_task":  "Run read-only research task",
	"parallel_tasks":  "Run parallel subagent tasks",
	"explore":         "Explore the codebase",
	"locate":          "Locate code in the codebase",
	"research":        "Research external references",
	"review":          "Review the current changes",
	"security_review": "Review security risks",
}
