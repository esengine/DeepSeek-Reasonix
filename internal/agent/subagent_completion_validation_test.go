package agent

import (
	"sync"
	"testing"

	"reasonix/internal/event"
)

type completionAuditRecordingSink struct {
	mu          sync.Mutex
	validations []event.CompletionValidationInfo
}

func (*completionAuditRecordingSink) Emit(event.Event) {}

func (s *completionAuditRecordingSink) RecordCompletionValidation(info event.CompletionValidationInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validations = append(s.validations, info)
}

func TestSubagentProgressForwardsCompletionValidation(t *testing.T) {
	parent := &completionAuditRecordingSink{}
	tracker := &subagentProgressTracker{sink: parent}
	event.RecordCompletionValidation(tracker.wrap(), event.CompletionValidationInfo{
		Mode: CompletionValidationEnforce, Outcome: "complete", Attempt: 1,
	})

	parent.mu.Lock()
	defer parent.mu.Unlock()
	if len(parent.validations) != 1 || parent.validations[0].Outcome != "complete" {
		t.Fatalf("completion validation audit = %+v, want one forwarded audit", parent.validations)
	}
}
