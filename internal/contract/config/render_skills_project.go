package config

import (
	"fmt"
	"reflect"
	"strings"
)

func renderProjectSkillsConfig(b *strings.Builder, c, d *Config) {
	// [skills]
	if !reflect.DeepEqual(c.Skills, d.Skills) || len(c.explicitProjectSkillKeys) > 0 {
		b.WriteString("[skills]\n")
		if len(c.Skills.Paths) > 0 || c.keepsProjectSkillKey("paths") {
			fmt.Fprintf(b, "paths = %s\n", renderStringArray(c.Skills.Paths))
		}
		if len(c.Skills.ExcludedPaths) > 0 || c.keepsProjectSkillKey("excluded_paths") {
			fmt.Fprintf(b, "excluded_paths = %s\n", renderStringArray(c.Skills.ExcludedPaths))
		}
		if c.Skills.DisableImplicitInvocation || c.keepsProjectSkillKey("disable_implicit_invocation") {
			fmt.Fprintf(b, "disable_implicit_invocation = %t\n", c.Skills.DisableImplicitInvocation)
		}
		if c.Skills.SuppressWarnings || c.keepsProjectSkillKey("suppress_warnings") {
			fmt.Fprintf(b, "suppress_warnings = %t\n", c.Skills.SuppressWarnings)
		}
		if c.Skills.MaxDepth != 0 || c.keepsProjectSkillKey("max_depth") {
			depth := c.Skills.MaxDepth
			if depth != 0 {
				depth = c.SkillMaxDepth()
			}
			fmt.Fprintf(b, "max_depth = %d\n", depth)
		}
		if disabled := c.DisabledSkillNames(); len(disabled) > 0 || c.keepsProjectSkillKey("disabled_skills") {
			fmt.Fprintf(b, "disabled_skills = %s\n\n", renderStringArray(disabled))
		}
	}

}
