package skill

import (
	"strings"

	"reasonix/internal/tool"
)

// ToolBindingsForSkill applies the same ownership boundary to invocation and
// diagnostics. Portable aliases are bound only for plugin-owned skills.
func ToolBindingsForSkill(sk Skill, bindings []tool.MCPBinding) []tool.MCPBinding {
	if strings.TrimSpace(sk.Plugin) == "" {
		return nil
	}
	var out []tool.MCPBinding
	for _, binding := range bindings {
		if binding.Package == sk.Plugin {
			out = append(out, binding)
		}
	}
	return out
}
