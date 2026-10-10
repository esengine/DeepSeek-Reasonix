// hook_notice.go — a hook outcome as the notice a frontend renders.
package boot

import (
	"errors"
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/hook"
)

// hookNoticeEvent maps what a hook did onto the notice contract: a stable code
// frontends localize by, the English headline they fall back to, and the hook's
// own words as detail a surface can put behind a disclosure. Flattening all
// three into Text is what turned a broken hook into one unreadable line.
func hookNoticeEvent(n hook.Notice) event.Event {
	return event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   hookNoticeCode(n),
		Text:   n.Text,
		Detail: n.Detail,
	}
}

func hookNoticeCode(n hook.Notice) string {
	if errors.Is(n.Refusal, config.ErrProjectProgramChanged) {
		return event.NoticeCodeProjectProgramChanged
	}
	switch n.Decision {
	case hook.DecisionBlock:
		if n.Cause != nil {
			return event.NoticeCodeHookUnevaluable
		}
		return event.NoticeCodeHookBlocked
	case hook.DecisionError:
		return event.NoticeCodeHookFailed
	default:
		return event.NoticeCodeHookWarned
	}
}

// projectHooksHeldEvent says which project hooks did not load and how to let them.
func projectHooksHeldEvent(p config.ProjectProgram) event.Event {
	return event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   event.NoticeCodeProjectProgramsAwaitingApproval,
		Text:   "This project's hooks are off until you approve them.",
		Detail: fmt.Sprintf("%s declares: %s\nRun `reasonix trust` in this workspace to review and approve them; any later change needs approval again.", p.Name, p.Detail),
	}
}
