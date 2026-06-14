package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/hook"
)

func renderHooks(width int, hooks []hook.ResolvedHook, trusted bool, projectDefines bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("hooks (%d active)", len(hooks)))
	for _, h := range hooks {
		match := h.Match
		if h.Event == hook.PreToolUse || h.Event == hook.PostToolUse {
			if match == "" {
				match = "*"
			}
		} else {
			match = "-"
		}
		used := 2 + viewPadWidth(string(h.Event), 16) + 1 + 8 + 1 + 8 + 1
		fmt.Fprintf(&b, "  %-16s %s %s %s\n",
			h.Event, viewMeta(fmt.Sprintf("%-8s", h.Scope)), viewMeta(fmt.Sprintf("%-8s", match)), viewCompactText(h.Command, viewBudget(width, used)))
	}
	b.WriteByte('\n')
	switch {
	case projectDefines && !trusted:
		b.WriteString(viewHint(viewCompactText("project hooks are not trusted; run /hooks trust to enable shell-command hooks", viewBudget(width, 2))))
	case trusted:
		b.WriteString(viewHint(viewCompactText("project trusted · config: project .reasonix/settings.json + global ~/.reasonix/settings.json", viewBudget(width, 2))))
	default:
		b.WriteString(viewHint(viewCompactText("project not trusted · config: project .reasonix/settings.json + global ~/.reasonix/settings.json", viewBudget(width, 2))))
	}
	return strings.TrimRight(b.String(), "\n")
}
