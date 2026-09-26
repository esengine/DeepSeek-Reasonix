package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/permission"
)

// emitUnmatchableRuleNotice warns about permission rules that name a tool
// nothing answers to. A deny written as a bare shell command installs cleanly
// and never fires, so the author is told rather than left believing the rule
// guards them. The notice is a warning, not an error: the rest of the policy
// is still valid and startup continues.
func emitUnmatchableRuleNotice(sink event.Sink, allow, ask, deny []string) {
	bad := permission.UnmatchableRules(allow, ask, deny)
	if len(bad) == 0 || sink == nil {
		return
	}
	var b strings.Builder
	for i, r := range bad {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "permissions.%s: %q names no tool and never matches; write %s instead", r.List, r.Rule, r.Suggestion)
	}
	sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Text:   fmt.Sprintf("%d permission rule(s) match nothing.", len(bad)),
		Detail: b.String(),
	})
}
