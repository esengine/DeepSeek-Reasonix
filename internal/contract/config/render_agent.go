package config

import (
	"fmt"
	"reflect"
	"strings"
)

// renderAgentDelta writes the first half of the [agent] scalar delta onto buf
// and reports whether it wrote any line. Split from renderAgentTail so neither
// half carries more branches than the complexity limit allows.
func renderAgentDelta(buf *strings.Builder, c, d *Config) bool {
	wrote := false
	if sp := strings.TrimSpace(c.Agent.SystemPrompt); sp != "" && sp != d.Agent.SystemPrompt {
		buf.WriteString("system_prompt = " + tomlMultilineBasicString(sp) + "\n")
		wrote = true
	}
	if c.Agent.SystemPromptFile != "" && c.Agent.SystemPromptFile != d.Agent.SystemPromptFile {
		fmt.Fprintf(buf, "system_prompt_file = %q\n", c.Agent.SystemPromptFile)
		wrote = true
	}
	if c.Agent.Temperature != d.Agent.Temperature {
		fmt.Fprintf(buf, "temperature = %s\n", formatFloat(c.Agent.Temperature))
		wrote = true
	}
	if c.Agent.RecoveryModel != "" && c.Agent.RecoveryModel != d.Agent.RecoveryModel {
		fmt.Fprintf(buf, "recovery_model = %q\n", c.Agent.RecoveryModel)
		wrote = true
	}
	if c.Agent.ReasoningLanguage != d.Agent.ReasoningLanguage {
		if l := c.ReasoningLanguage(); l != "auto" {
			fmt.Fprintf(buf, "reasoning_language = %q\n", l)
			wrote = true
		}
	}
	if c.Agent.CompactRatio != d.Agent.CompactRatio {
		fmt.Fprintf(buf, "compact_ratio = %s\n", formatFloat(c.Agent.CompactRatio))
		wrote = true
	}
	if c.Agent.Keep != nil && !reflect.DeepEqual(c.Agent.Keep, d.Agent.Keep) {
		fmt.Fprintf(buf, "keep = %s\n", renderStringArray(c.Agent.Keep))
		wrote = true
	}
	if c.Agent.RecentKeep > 0 && c.Agent.RecentKeep != d.Agent.RecentKeep {
		fmt.Fprintf(buf, "recent_keep = %d\n", c.Agent.RecentKeep)
		wrote = true
	}
	if len(c.Agent.PlanModeReadOnlyCommands) > 0 && !reflect.DeepEqual(c.Agent.PlanModeReadOnlyCommands, d.Agent.PlanModeReadOnlyCommands) {
		fmt.Fprintf(buf, "plan_mode_read_only_commands = %s\n", renderStringArray(c.Agent.PlanModeReadOnlyCommands))
		wrote = true
	}
	return wrote
}

// renderAgentTail writes the rest of the [agent] scalar delta, including the
// loop-guard budget, and reports whether it wrote any line.
func renderAgentTail(buf *strings.Builder, c, d *Config) bool {
	wrote := false
	if c.Agent.TitleModel != "" && c.Agent.TitleModel != d.Agent.TitleModel {
		fmt.Fprintf(buf, "title_model = %q\n", c.Agent.TitleModel)
		wrote = true
	}
	if len(c.Agent.RoleEfforts) > 0 && !reflect.DeepEqual(c.Agent.RoleEfforts, d.Agent.RoleEfforts) {
		fmt.Fprintf(buf, "role_efforts = %s\n", renderStringMap(c.Agent.RoleEfforts))
		wrote = true
	}
	if c.Agent.PlannerModel != "" && c.Agent.PlannerModel != d.Agent.PlannerModel {
		fmt.Fprintf(buf, "planner_model = %q\n", c.Agent.PlannerModel)
		wrote = true
	}
	if c.Agent.SubagentModel != "" && c.Agent.SubagentModel != d.Agent.SubagentModel {
		fmt.Fprintf(buf, "subagent_model = %q\n", c.Agent.SubagentModel)
		wrote = true
	}
	if len(c.Agent.SubagentModels) > 0 && !reflect.DeepEqual(c.Agent.SubagentModels, d.Agent.SubagentModels) {
		fmt.Fprintf(buf, "subagent_models = %s\n", renderStringMap(c.Agent.SubagentModels))
		wrote = true
	}
	if c.Agent.SubagentEffort != "" && c.Agent.SubagentEffort != d.Agent.SubagentEffort {
		fmt.Fprintf(buf, "subagent_effort = %q\n", c.Agent.SubagentEffort)
		wrote = true
	}
	if len(c.Agent.SubagentEfforts) > 0 && !reflect.DeepEqual(c.Agent.SubagentEfforts, d.Agent.SubagentEfforts) {
		fmt.Fprintf(buf, "subagent_efforts = %s\n", renderStringMap(c.Agent.SubagentEfforts))
		wrote = true
	}
	if c.Agent.MaxSubagentDepth != d.Agent.MaxSubagentDepth {
		fmt.Fprintf(buf, "max_subagent_depth = %d\n", c.Agent.MaxSubagentDepth)
		wrote = true
	}
	if c.Agent.OutputStyle != "" && c.Agent.OutputStyle != d.Agent.OutputStyle {
		fmt.Fprintf(buf, "output_style = %q\n", c.Agent.OutputStyle)
		wrote = true
	}
	return wrote
}

// tomlMultilineBasicString writes s as a """ string that decodes back to s:
// backslashes, control characters and any quote that could close the string
// early are escaped, and line breaks stay literal.
func tomlMultilineBasicString(s string) string {
	var b strings.Builder
	b.WriteString("\"\"\"\n")
	for i := range len(s) {
		switch ch := s[i]; {
		case ch == '\\':
			b.WriteString(`\\`)
		case ch == '"' && (i == len(s)-1 || s[i+1] == '"'):
			b.WriteString(`\"`)
		case ch == '\n' || ch == '\t':
			b.WriteByte(ch)
		case ch < 0x20 || ch == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString(`"""`)
	return b.String()
}
