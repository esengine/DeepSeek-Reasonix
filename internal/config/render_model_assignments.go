package config

import (
	"fmt"
	"reflect"
	"strings"
)

func renderAgentModelAssignments(b *strings.Builder, c *Config) {
	if c.Agent.PlannerModel != "" {
		fmt.Fprintf(b, "planner_model = %q   # low-frequency planner (two-model collaboration)\n", c.Agent.PlannerModel)
	} else {
		b.WriteString("# planner_model = \"deepseek-pro\"   # optional: enable two-model collaboration\n")
	}
	if c.Agent.WebSearchModel != "" {
		fmt.Fprintf(b, "web_search_model = %q\n", c.Agent.WebSearchModel)
	}
	if c.Agent.VisionModel != "" {
		fmt.Fprintf(b, "vision_model = %q   # image understanding fallback: auto or provider/model\n", c.Agent.VisionModel)
	} else {
		b.WriteString("# vision_model = \"auto\"   # optional: summarize images for text-only models\n")
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
}

func renderAgentModelAssignmentDelta(b *strings.Builder, c, d *Config, anyAgent *bool) {
	if c.Agent.PlannerModel != "" && c.Agent.PlannerModel != d.Agent.PlannerModel {
		fmt.Fprintf(b, "planner_model = %q\n", c.Agent.PlannerModel)
		*anyAgent = true
	}
	if c.Agent.WebSearchModel != d.Agent.WebSearchModel {
		fmt.Fprintf(b, "web_search_model = %q\n", c.Agent.WebSearchModel)
		*anyAgent = true
	}
	if c.Agent.VisionModel != d.Agent.VisionModel {
		fmt.Fprintf(b, "vision_model = %q\n", c.Agent.VisionModel)
		*anyAgent = true
	}
	if c.Agent.SubagentModel != "" && c.Agent.SubagentModel != d.Agent.SubagentModel {
		fmt.Fprintf(b, "subagent_model = %q\n", c.Agent.SubagentModel)
		*anyAgent = true
	}
	if len(c.Agent.SubagentModels) > 0 && !reflect.DeepEqual(c.Agent.SubagentModels, d.Agent.SubagentModels) {
		fmt.Fprintf(b, "subagent_models = %s\n", renderStringMap(c.Agent.SubagentModels))
		*anyAgent = true
	}
}
