// render_delegation.go — the [agent] keys that hand work to another model.
package config

import (
	"fmt"
	"strings"
)

// renderAgentDelegation writes every key that names a model other than the main
// one, plus the budgets that bound what those handoffs may spend. Kept together
// because they are read together: a role with no line here is a setting the API
// accepts, saves without error, and loses on the next read.
func renderAgentDelegation(b *strings.Builder, c *Config) {
	if c.Agent.PlannerModel != "" {
		fmt.Fprintf(b, "planner_model = %q   # low-frequency planner (two-model collaboration)\n", c.Agent.PlannerModel)
	} else {
		b.WriteString("# planner_model = \"deepseek-pro\"   # optional: enable two-model collaboration\n")
	}
	if c.Agent.SubagentModel != "" {
		fmt.Fprintf(b, "subagent_model = %q   # default model for runAs=subagent skills\n", c.Agent.SubagentModel)
	} else {
		b.WriteString("# subagent_model = \"deepseek-pro\"   # optional default for runAs=subagent skills\n")
	}
	if len(c.Agent.SubagentModels) > 0 {
		fmt.Fprintf(b, "subagent_models = %s   # per-skill overrides\n", renderStringMap(c.Agent.SubagentModels))
	} else {
		b.WriteString("# subagent_models = { review = \"deepseek-pro\", security_review = \"deepseek-pro\" }   # per-skill overrides\n")
	}
	if c.Agent.VisionModel != "" {
		fmt.Fprintf(b, "vision_model = %q   # reads images a text-only main model cannot\n", c.Agent.VisionModel)
	} else {
		b.WriteString("# vision_model = \"kimi/kimi-k2-vision\"   # optional: reads images a text-only main model cannot\n")
	}
	if c.Agent.GuardianModel != "" {
		fmt.Fprintf(b, "guardian_model = %q   # independent reviewer for this turn's risky calls\n", c.Agent.GuardianModel)
	} else {
		b.WriteString("# guardian_model = \"deepseek-pro\"   # optional: independent reviewer for risky calls\n")
	}
	if c.Agent.GuardianTemperature != 0 {
		fmt.Fprintf(b, "guardian_temperature = %s   # the reviewer's own sampling temperature\n", formatFloat(c.Agent.GuardianTemperature))
	} else {
		b.WriteString("# guardian_temperature = 0.0   # the reviewer's own sampling temperature\n")
	}
	if c.Agent.DecisionModel != "" {
		fmt.Fprintf(b, "decision_model = %q   # the system_one backend; a decision wire, never the main model\n", c.Agent.DecisionModel)
	} else {
		b.WriteString("# decision_model = \"typesafe/system-one\"   # optional: the system_one backend\n")
	}
	if c.Agent.TriageModel != "" {
		fmt.Fprintf(b, "triage_model = %q   # small classifications; falls back to subagent, then main\n", c.Agent.TriageModel)
	} else {
		b.WriteString("# triage_model = \"deepseek-flash\"   # small classifications; point it somewhere cheap\n")
	}
	if c.Agent.AdvisorModel != "" {
		fmt.Fprintf(b, "advisor_model = %q   # the stronger model the advise tool consults\n", c.Agent.AdvisorModel)
	} else {
		b.WriteString("# advisor_model = \"deepseek-pro\"   # optional: offers the advise tool, backed by this model\n")
	}
	if c.Agent.BestOfN {
		b.WriteString("best_of_n = true   # parallel attempts in git worktrees; a judge applies the best\n")
	} else {
		b.WriteString("# best_of_n = true   # parallel attempts in git worktrees; a judge applies the best\n")
	}
	if c.Agent.WorktreeIsolation {
		b.WriteString("worktree_isolation = true   # task can run a writer in a git worktree; changes wait for apply_isolated\n")
	} else {
		b.WriteString("# worktree_isolation = true   # task can run a writer in a git worktree; changes wait for apply_isolated\n")
	}
	if c.Agent.CodeMode {
		b.WriteString("code_mode = true   # run_script: tool calls from a short script, one round trip\n")
	} else {
		b.WriteString("# code_mode = true   # run_script: tool calls from a short script, one round trip\n")
	}
	if c.Agent.ScreenExternalContent {
		b.WriteString("screen_external_content = true   # triage flags external results that address the agent\n")
	} else {
		b.WriteString("# screen_external_content = true   # triage flags external results that address the agent\n")
	}
	if c.Agent.TaskCostBudget > 0 {
		fmt.Fprintf(b, "task_cost_budget = %s   # a task lands on one summary once it spends this much\n", formatFloat(c.Agent.TaskCostBudget))
	} else {
		b.WriteString("# task_cost_budget = 0.50   # a task lands on one summary once it spends this much\n")
	}
	if c.Agent.TaskTimeBudgetMinutes > 0 {
		fmt.Fprintf(b, "task_time_budget_minutes = %s   # the same landing, measured in wall clock\n", formatFloat(c.Agent.TaskTimeBudgetMinutes))
	} else {
		b.WriteString("# task_time_budget_minutes = 10   # the same landing, measured in wall clock\n")
	}
	if c.Agent.TaskTokenBudget > 0 {
		fmt.Fprintf(b, "task_token_budget = %d   # the same landing, measured in cumulative model tokens\n", c.Agent.TaskTokenBudget)
	} else {
		b.WriteString("# task_token_budget = 2000000   # the same landing, measured in cumulative model tokens\n")
	}
	if c.Agent.GoalTokenBudget > 0 {
		fmt.Fprintf(b, "goal_token_budget = %d   # what a /goal loop may spend before it stops\n", c.Agent.GoalTokenBudget)
	} else {
		b.WriteString("# goal_token_budget = 200000   # what a /goal loop may spend before it stops\n")
	}
}
