package agent

import (
	"testing"

	"reasonix/internal/event"
)

type plannerAuditCapture struct {
	event.FuncSink
	records []event.CompletionValidationInfo
}

func (s *plannerAuditCapture) RecordCompletionValidation(info event.CompletionValidationInfo) {
	s.records = append(s.records, info)
}

func TestPlannerSinkForwardsCompletionValidationAudit(t *testing.T) {
	capture := &plannerAuditCapture{}
	sink := plannerSink(capture)
	want := event.CompletionValidationInfo{Mode: "enforce", Outcome: "continue", Attempt: 1}
	event.RecordCompletionValidation(sink, want)
	if len(capture.records) != 1 || capture.records[0] != want {
		t.Fatalf("planner completion audits = %+v, want %+v", capture.records, want)
	}
}
