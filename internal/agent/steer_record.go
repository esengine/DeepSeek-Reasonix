package agent

import (
	"context"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

// UnappliedSteerNotice returns the durable warning shown for guidance that was
// accepted during an abnormal turn exit but never reached a provider request.
// The user's guidance rides the format's trailing %s so fronts can split it
// back out at the first newline.
func UnappliedSteerNotice(text string) string {
	return fmt.Sprintf(i18n.M.UnappliedSteerFmt, text)
}

// RecordUnappliedSteer stores guidance that could not affect its intended
// in-flight turn. The orphan-tool sentinel makes older readers drop the record
// during wire normalization, while current readers use LocalOnly to exclude it
// before every provider request. itemID correlates the notice with the durable
// session inbox entry when one exists.
func (a *Agent) RecordUnappliedSteer(text string, itemID ...string) {
	if a == nil || a.sess.conversation == nil {
		return
	}
	id := ""
	if len(itemID) > 0 {
		id = itemID[0]
	}
	_ = a.appendCommittedMessages(context.Background(), "unapplied-steer", provider.Message{
		Role:       provider.RoleTool,
		Content:    a.withTurnPreferences(midTurnSteerMessage(text)),
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
	})
	a.svc.sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   event.NoticeCodeUnappliedSteer,
		Text:   UnappliedSteerNotice(text),
		ItemID: id,
	})
}
