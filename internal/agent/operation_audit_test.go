package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// operationAuditSink collects the lifecycle counters a run produced.
type operationAuditSink struct {
	event.Sink
	audits *[]evidence.OperationAudit
}

func (s operationAuditSink) RecordOperationAudit(a evidence.OperationAudit) {
	*s.audits = append(*s.audits, a)
}

func auditingAgent(t *testing.T, writer tool.Tool) (*Agent, *[]evidence.OperationAudit) {
	t.Helper()
	audits := &[]evidence.OperationAudit{}
	reg := tool.NewRegistry()
	reg.Add(writer)
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{}, operationAuditSink{event.Discard, audits})
	a.task.ledger = evidence.NewLedger()
	a.reads.gates = true
	return a, audits
}

func metricCount(audits []evidence.OperationAudit, metric string) int {
	count := 0
	for _, a := range audits {
		if a.Metric == metric {
			count++
		}
	}
	return count
}

func TestOperationCountersRecordSettlementAndPause(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, audits := auditingAgent(t, writer)

	// An ordinary mutation settles and is counted once.
	args := `{"path":"internal/auth/login.go"}`
	rec := evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		Paths:       []string{"internal/auth/login.go"},
		OperationID: evidence.OperationID("write_file", json.RawMessage(args)),
	}
	a.recordOperationOutcome(&toolCallPlan{call: provider.ToolCall{Name: "write_file", Arguments: args}}, rec, nil)
	if got := metricCount(*audits, evidence.MetricOperationSettled); got != 1 {
		t.Fatalf("%s = %d, want 1", evidence.MetricOperationSettled, got)
	}

	// Two identical rejections: one recovery attempt, then the pause.
	runEvidenceGate(a, "/w/a.go")
	runEvidenceGate(a, "/w/a.go")
	if got := metricCount(*audits, evidence.MetricOperationRecoveryAttempt); got != 1 {
		t.Fatalf("%s = %d, want 1", evidence.MetricOperationRecoveryAttempt, got)
	}
	if got := metricCount(*audits, evidence.MetricOperationNeedsUser); got != 1 {
		t.Fatalf("%s = %d, want 1", evidence.MetricOperationNeedsUser, got)
	}

	// A third attempt is refused without running, and counted as such.
	a.applyOperationGate(writeOperationPlan("/w/a.go"))
	if got := metricCount(*audits, evidence.MetricOperationDuplicateBlock); got != 1 {
		t.Fatalf("%s = %d, want 1", evidence.MetricOperationDuplicateBlock, got)
	}
}

func TestOperationCountersRecordUnclassifiedCommands(t *testing.T) {
	a, audits := auditingAgent(t, evidenceWriter{})
	args := `{"command":"./company-ci.sh"}`
	rec := evidence.Receipt{
		ToolName: "bash", Success: true, Command: "./company-ci.sh",
		OperationID: evidence.OperationID("bash", json.RawMessage(args)),
	}

	a.recordOperationOutcome(&toolCallPlan{call: provider.ToolCall{Name: "bash", Arguments: args}}, rec, nil)

	if got := metricCount(*audits, evidence.MetricVerificationUnclassified); got != 1 {
		t.Fatalf("%s = %d, want 1", evidence.MetricVerificationUnclassified, got)
	}
}

func TestOperationCountersCarryNoContent(t *testing.T) {
	a, audits := auditingAgent(t, evidenceWriter{})
	args := `{"path":"/secret/workspace/login.go"}`
	rec := evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		Paths:       []string{"/secret/workspace/login.go"},
		OperationID: evidence.OperationID("write_file", json.RawMessage(args)),
	}

	a.recordOperationOutcome(&toolCallPlan{call: provider.ToolCall{Name: "write_file", Arguments: args}}, rec, nil)

	for _, audit := range *audits {
		encoded, err := json.Marshal(audit)
		if err != nil {
			t.Fatal(err)
		}
		if containsAny(string(encoded), "/secret/workspace", "login.go") {
			t.Fatalf("audit leaked a local path: %s", encoded)
		}
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
	}
	return false
}
