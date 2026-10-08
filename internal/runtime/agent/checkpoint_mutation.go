package agent

import "reasonix/internal/contract/event"

func (a *Agent) emitCheckpointWarning(err error) {
	if err == nil {
		return
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn, Audience: event.NoticeAudienceOperator,
		Code:   event.NoticeCodeCheckpointRecordingFailed,
		Text:   "Checkpoint recording failed; the tool execution result is unchanged",
		Detail: err.Error(),
	})
}
