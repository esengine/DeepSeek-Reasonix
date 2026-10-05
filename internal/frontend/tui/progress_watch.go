package tui

import (
	"fmt"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// foldProgressWatch says a stall once, when it starts. The kernel reports it
// every round it lasts; repeating that would bury the transcript it is about,
// and a report that it cleared needs no row of its own.
func (t *Transcript) foldProgressWatch(w *eventwire.ProgressWatch) {
	if w == nil || !w.Stalled {
		t.stallSaid = false
		return
	}
	if t.stallSaid {
		return
	}
	t.stallSaid = true
	t.foldNotice(eventwire.Event{Kind: "notice", Level: "warn", Code: "progress_watch", Text: stallText(w)})
}

func stallText(w *eventwire.ProgressWatch) string {
	switch w.Cause {
	case "tokens":
		return fmt.Sprintf(i18n.M.TUIStallTokensFmt, w.TokenMultiple, w.PromptTokens)
	case "perseveration":
		return i18n.M.TUIStallRepeating
	default:
		return fmt.Sprintf(i18n.M.TUIStallIdleFmt, w.IdleRounds)
	}
}
