package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/outputstyle"
)

func renderOutputStyles(width int, styles []outputstyle.OutputStyle, active string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("output styles"))
	for _, st := range styles {
		scope := "builtin"
		if !st.Builtin {
			scope = "custom"
		}
		status := ""
		if strings.EqualFold(st.Name, active) {
			status = "  " + viewStatus("active")
		}
		scopeText := "(" + scope + ")"
		used := 2 + viewPadWidth(st.Name, 16) + 1 + visibleWidth(scopeText) + 2 + visibleWidth(status)
		desc := viewCompactText(st.Description, viewBudget(width, used))
		fmt.Fprintf(&b, "  %-16s %s  %s%s\n", st.Name, viewMeta(scopeText), desc, status)
	}
	b.WriteString(viewHint(viewCompactText("set agent.output_style in reasonix.toml to apply one (takes effect next session)", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}
