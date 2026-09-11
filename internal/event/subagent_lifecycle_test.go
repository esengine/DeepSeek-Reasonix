package event

import "testing"

type lifecycleAuditSink struct {
	info []SubagentLifecycleInfo
}

func (s *lifecycleAuditSink) Emit(Event) {}

func (s *lifecycleAuditSink) RecordSubagentLifecycle(info SubagentLifecycleInfo) {
	s.info = append(s.info, info)
}

func TestRecordSubagentLifecycleForwardsThroughWrappers(t *testing.T) {
	inner := &lifecycleAuditSink{}
	wrapped := Sync(inner)
	want := SubagentLifecycleInfo{Phase: "child_partial", Ref: "sa_child", Status: "partial", Retryable: true}
	RecordSubagentLifecycle(wrapped, want)
	if len(inner.info) != 1 || inner.info[0] != want {
		t.Fatalf("lifecycle audit = %+v, want %+v", inner.info, want)
	}
}
