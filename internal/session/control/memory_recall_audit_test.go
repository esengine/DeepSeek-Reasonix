package control

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

type recallAuditSink struct {
	event.Sink
	audits []event.MemoryRecallAudit
}

func (s *recallAuditSink) RecordMemoryRecall(a event.MemoryRecallAudit) {
	s.audits = append(s.audits, a)
}

func TestComposeEmitsMemoryRecallAudit(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	sink := &recallAuditSink{Sink: event.Discard}
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	_ = c.Compose("what is the canonical fast check command")
	if len(sink.audits) != 1 {
		t.Fatalf("audits = %d, want exactly one recall audit per composed user turn", len(sink.audits))
	}
	if sink.audits[0].Suppressed == "" && len(sink.audits[0].Hits) == 0 {
		t.Fatalf("audit must explain itself: %+v", sink.audits[0])
	}
}
