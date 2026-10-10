package control

import (
	"context"
	"errors"
	"log/slog"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
)

var errCompactBusy = errors.New("cannot compact while a turn is running or the session is changing")

// compactAndReport folds the context and says what happened. A fold the kernel
// declined is an answer about this transcript — nothing left worth folding —
// and reporting it as a failure sent people looking for a broken kernel.
func (c *Controller) compactAndReport(focus string) {
	verdict, err := c.Compact(context.Background(), agent.CompactRequest{Instructions: focus})
	switch {
	case err == nil && verdict.Compacted():
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeCompacted, Text: "compacted"})
		if err := c.SnapshotRewrite(); err != nil {
			slog.Warn("controller: snapshot after compact", "err", err)
		}
	case err == nil:
		c.noticeCompactDeclined(verdict.Reason, agent.CompactDeclineText(verdict.Reason))
	case agent.IsCompactionDeclined(err):
		c.noticeCompactDeclined(agent.CompactionFailureCode(err), agent.CompactionDeclineReason(err))
	default:
		code := agent.CompactionFailureCode(err)
		if errors.Is(err, errCompactBusy) {
			code = agent.FailBusy
		}
		slog.Warn("controller: compaction failed", "code", code, "err", err)
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeCompactFailed,
			Text: "compaction failed: " + err.Error(), Detail: string(code)})
	}
}

// noticeCompactDeclined says a fold was declined; the frontends word the code in
// their own language and print text only for a code they do not know.
func (c *Controller) noticeCompactDeclined(code agent.CompactionNoopReason, text string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeCompactDeclined,
		Text: "nothing to compact — " + text, Detail: string(code)})
}
