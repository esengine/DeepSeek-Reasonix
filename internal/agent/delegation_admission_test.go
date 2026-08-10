package agent

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestDelegationAdmissionVerdicts(t *testing.T) {
	cases := []struct {
		name, input, args, verdict, reason string
	}{
		{"local fix, plain query", "fix the config serializer bug in parser.go",
			`{"prompt":"how does the serializer format keys"}`, "deny", "local_fix_no_external_need"},
		{"user asked for research", "research the best TOML library and fix the loader",
			`{"prompt":"compare toml libraries"}`, "allow", "user_requested"},
		{"external source cited", "fix the retry logic to match the upstream spec",
			`{"prompt":"read https://example.com/spec and summarize backoff rules"}`, "allow", "external_source_cited"},
		{"advisory turn", "how does our retry budget compare to industry practice?",
			`{"prompt":"survey retry budget conventions"}`, "allow", "non_local_intent"},
	}
	for _, c := range cases {
		verdict, reason, _ := delegationAdmission(c.input, c.args)
		if verdict != c.verdict || reason != c.reason {
			t.Errorf("%s: got %s/%s, want %s/%s", c.name, verdict, reason, c.verdict, c.reason)
		}
	}
}

type admissionSink struct {
	audits []event.DelegationAdmissionAudit
}

func (s *admissionSink) Emit(event.Event) {}
func (s *admissionSink) RecordDelegationAdmission(a event.DelegationAdmissionAudit) {
	s.audits = append(s.audits, a)
}

func TestObserveDelegationAdmissionRecordsOnlyGatedTools(t *testing.T) {
	sink := &admissionSink{}
	a := &Agent{sink: sink}
	a.recoveryTaskSummary = "fix the failing date parser"
	a.observeDelegationAdmission([]provider.ToolCall{
		{Name: "read_file", Arguments: `{"path":"a.go"}`},
		{Name: "research", Arguments: `{"prompt":"date formats"}`},
		{Name: "task", Arguments: `{"prompt":"sub work"}`},
	})
	if len(sink.audits) != 1 {
		t.Fatalf("got %d audits, want 1 (research only)", len(sink.audits))
	}
	got := sink.audits[0]
	if got.Tool != "research" || got.Verdict != "deny" || got.Reason != "local_fix_no_external_need" || got.Intent != "mutation" {
		t.Fatalf("audit = %+v, want deny/local_fix_no_external_need on mutation intent", got)
	}
}
