package event

import "reasonix/internal/nilutil"

// SubagentLifecycleInfo is content-free host telemetry for one child
// transition. It intentionally excludes prompts, reasoning, tool output, and
// paths so it can be forwarded to diagnostics without leaking transcript data.
type SubagentLifecycleInfo struct {
	Phase             string // child_created | child_running | child_completed | child_partial | child_failed | child_cancelled | child_resume
	Ref               string
	ParentToolCallID  string
	Skill             string
	Model             string
	Effort            string
	Status            string
	ErrorCode         string
	Retryable         bool
	OutputBytes       int
	StartUnixMs       int64
	EndUnixMs         int64
	ValidatorMode     string
	ValidatorOutcome  string
	ValidatorAttempt  int
	ProviderRequestID string
}

// SubagentLifecycleAuditSink receives host-only child lifecycle telemetry.
type SubagentLifecycleAuditSink interface {
	RecordSubagentLifecycle(SubagentLifecycleInfo)
}

// RecordSubagentLifecycle forwards content-free child lifecycle telemetry to
// sinks that explicitly opt in.
func RecordSubagentLifecycle(s Sink, info SubagentLifecycleInfo) {
	if nilutil.IsNil(s) {
		return
	}
	if ls, ok := s.(SubagentLifecycleAuditSink); ok {
		ls.RecordSubagentLifecycle(info)
	}
}
