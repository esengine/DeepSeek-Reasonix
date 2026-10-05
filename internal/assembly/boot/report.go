package boot

import "reasonix/internal/contract/event"

// report is how assembly speaks: which model was chosen, which server failed to
// start, which migration ran — none of it part of the conversation the runtime
// goes on to have. Stamping the audience here rather than at two dozen call
// sites is what keeps the next one from forgetting; notice_audience_test is
// what keeps it from being bypassed.
func report(sink event.Sink, e event.Event) {
	if sink == nil {
		return
	}
	e.Kind = event.Notice
	e.Audience = event.NoticeAudienceOperator
	sink.Emit(e)
}
