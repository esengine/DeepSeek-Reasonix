package agent

import "testing"

// The six shapes a stored child can arrive in, and what each one may be joined
// on. Reads are lenient about records written before executions were named
// apart; writes are not, because a record no reader can place is exactly what
// the separation exists to stop producing.
func TestResolveExecutionIdentityIsEvidenceDriven(t *testing.T) {
	opened := func(ids ...string) func(string) bool {
		set := map[string]bool{}
		for _, id := range ids {
			set[id] = true
		}
		return func(id string) bool { return set[id] }
	}
	for _, tc := range []struct {
		name       string
		meta       SubagentMeta
		journal    func(string) bool
		wantID     string
		wantSource string
	}{
		{
			name:       "a delegated run names its own execution",
			meta:       SubagentMeta{ExecutionID: "call-1/sub-1", ParentToolCallID: "call-1/sub-1"},
			journal:    opened("call-1/sub-1"),
			wantID:     "call-1/sub-1",
			wantSource: ExecutionRecorded,
		},
		{
			name:       "a host-started run names one with no parent call",
			meta:       SubagentMeta{ExecutionID: "host-abc"},
			journal:    opened("host-abc"),
			wantID:     "host-abc",
			wantSource: ExecutionRecorded,
		},
		{
			// The older writer used one id for both, and the journal is what
			// shows it: a parent id alone proves only what that writer meant by
			// lineage.
			name:       "a legacy record whose parent the journal also opened",
			meta:       SubagentMeta{ParentToolCallID: "call-1/sub-1"},
			journal:    opened("call-1/sub-1"),
			wantID:     "call-1/sub-1",
			wantSource: ExecutionLegacyConfirmed,
		},
		{
			// The shape a single task once wrote: the call in the store, the
			// node in the journal. A prefix rule would have joined these two,
			// and it would have been wrong.
			name:       "a legacy record whose parent names no execution",
			meta:       SubagentMeta{ParentToolCallID: "call-1"},
			journal:    opened("call-1/sub-1"),
			wantSource: ExecutionUnknown,
		},
		{
			name:       "a legacy host record, which never had an execution to name",
			meta:       SubagentMeta{},
			journal:    opened("call-1/sub-1"),
			wantSource: ExecutionUnknown,
		},
		{
			name:       "no journal to confirm anything",
			meta:       SubagentMeta{ParentToolCallID: "call-1/sub-1"},
			journal:    nil,
			wantSource: ExecutionUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveExecutionIdentity(tc.meta, tc.journal)
			if got.Execution != tc.wantID || got.Source != tc.wantSource {
				t.Fatalf("resolved %+v, want execution %q from %q", got, tc.wantID, tc.wantSource)
			}
			if got.Source == ExecutionUnknown && got.Execution != "" {
				t.Fatalf("an unknown identity carried %q; a caller joining on it would join on a guess", got.Execution)
			}
		})
	}
}

// Reads are lenient, writes are strict. A durable transcript for an execution
// nothing named is the one shape a later reader can never place, so it is
// refused where it would be created rather than tolerated and resolved forever.
func TestADurableRunMustNameItsExecution(t *testing.T) {
	spec := SubagentSpec{Kind: "task", Name: "task", ParentSession: "probe", SystemPrompt: "sys"}
	if err := requireExecution(spec); err == nil {
		t.Fatal("a durable run with no execution was accepted")
	}
	spec.ExecutionID = "host-abc"
	if err := requireExecution(spec); err != nil {
		t.Fatalf("a run naming its execution was refused: %v", err)
	}
}
