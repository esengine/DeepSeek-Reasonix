package serve

import (
	"net/http"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

// submitAs runs a submitted line as the one who sent it: a paired device's
// line lands as that device's, and the window's as its own.
func submitAs(ctrl control.SessionAPI, r *http.Request, input, format string, refuseUnknownSlash bool) {
	if refuseUnknownSlash {
		ctrl.SubmitHTTPOptions(input, control.SubmitOptions{Format: format, Via: viaOf(r), RefuseUnknownSlash: true})
		return
	}
	if via := viaOf(r); via != nil {
		ctrl.SubmitHTTPFrom(input, format, via)
		return
	}
	ctrl.SubmitHTTPFormat(input, format)
}

// approveAs answers a prompt as the one who answered it. The device changes
// who the receipt names, never what the answer allows.
func approveAs(ctrl control.SessionAPI, r *http.Request, id string, allow, session, persist bool) {
	if via := viaOf(r); via != nil {
		ctrl.ApproveFrom(id, allow, session, persist, via)
		return
	}
	ctrl.Approve(id, allow, session, persist)
}

// answerAs settles a question as the one who answered it.
func answerAs(ctrl control.SessionAPI, r *http.Request, id string, answers []event.AskAnswer) {
	if via := viaOf(r); via != nil {
		ctrl.AnswerQuestionFrom(id, answers, via)
		return
	}
	ctrl.AnswerQuestion(id, answers)
}
