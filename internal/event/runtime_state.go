package event

import "reasonix/internal/nilutil"

// RuntimeStateSnapshot is a host-only, replaceable observation. It is never a
// transcript or durable turn record. Running retains the legacy admission gate.
type RuntimeStateSnapshot struct {
	SchemaVersion   int        `json:"schemaVersion"`
	RuntimeEpoch    string     `json:"runtimeEpoch"`
	Revision        uint64     `json:"revision"`
	Phase           string     `json:"phase"`
	Running         bool       `json:"running"`
	TurnID          string     `json:"turnId"`
	TurnStatus      TurnStatus `json:"turnStatus"`
	TurnEventSeq    uint64     `json:"turnEventSeq"`
	PendingPrompt   bool       `json:"pendingPrompt"`
	CancelRequested bool       `json:"cancelRequested"`
	Cancellable     bool       `json:"cancellable"`
	BackgroundJobs  int        `json:"backgroundJobs"`
	Activity        string     `json:"activity"`
}

func (s RuntimeStateSnapshot) ActiveWork() bool {
	return s.Running || s.PendingPrompt || s.BackgroundJobs > 0
}

// RuntimeStateSink is independent of Emit: a state refresh must not become a
// new ledger record, extension invocation, or model-visible message.
type RuntimeStateSink interface{ RuntimeStateChanged(RuntimeStateSnapshot) }

func PublishRuntimeState(sink Sink, snapshot RuntimeStateSnapshot) {
	if nilutil.IsNil(sink) {
		return
	}
	if target, ok := sink.(RuntimeStateSink); ok {
		target.RuntimeStateChanged(snapshot)
	}
}

func (f AuditForwarder) RuntimeStateChanged(s RuntimeStateSnapshot) { PublishRuntimeState(f.Inner, s) }
func (s *syncSink) RuntimeStateChanged(snapshot RuntimeStateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	PublishRuntimeState(s.inner, snapshot)
}
func (c *coalescer) RuntimeStateChanged(snapshot RuntimeStateSnapshot) {
	c.enqueueCapability(func() { PublishRuntimeState(c.inner, snapshot) })
}
