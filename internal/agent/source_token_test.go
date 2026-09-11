package agent

import (
	"context"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func sourceTokenGate(a *Agent, path, token string) evidenceCheck {
	call := provider.ToolCall{Name: "write_file", Arguments: `{"path":"` + path + `","source_token":"` + token + `"}`}
	writer, _ := a.svc.tools.Get("write_file")
	return a.checkOperationEvidence(context.Background(), call, writer, a.task.ledger.ObservationBoundary())
}

func recordTokenObservation(ledger *evidence.Ledger, token, path, snapshot string, lines ...string) {
	ledger.RecordTextObservation(evidence.TextObservation{
		Path: path, StartLine: 1, Snapshot: snapshot, LineHashes: hashesFor(lines...), Token: token,
	})
}

func TestSourceTokenProvesTheVersionBeingReplaced(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss1", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/a.go", "ss1", "alpha", "beta")

	if check := sourceTokenGate(a, "/w/a.go", "r_read1"); !check.Satisfied {
		t.Fatalf("a token covering the exact current content was rejected: %+v", check)
	}
}

func TestSourceTokenFromAnotherVersionIsRejectedOnce(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss2", WholeFile: true, Hashes: hashesFor("alpha", "changed"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/a.go", "ss1", "alpha", "beta")

	check := sourceTokenGate(a, "/w/a.go", "r_read1")
	if check.Satisfied {
		t.Fatal("a stale token must not authorize overwriting newer content")
	}
	if check.Reason != "stale_or_partial_evidence" {
		t.Fatalf("reason = %q, want stale_or_partial_evidence", check.Reason)
	}
	if check.Recovery == "" {
		t.Fatal("a stale token must name the concrete recovery, not invite another attempt")
	}
}

func TestSourceTokenForAnotherFileProvesNothing(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss1", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/other.go", "ss1", "alpha", "beta")

	if check := sourceTokenGate(a, "/w/a.go", "r_read1"); check.Satisfied {
		t.Fatal("a token issued for a different file authorized this write")
	}
}

func TestInventedSourceTokenIsRejected(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss1", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/a.go", "ss1", "alpha", "beta")

	check := sourceTokenGate(a, "/w/a.go", "r_made_up")
	if check.Satisfied {
		t.Fatal("a token the host never issued must prove nothing")
	}
	if check.Reason != "source_token_unknown" {
		t.Fatalf("reason = %q, want source_token_unknown", check.Reason)
	}
}

func TestPartialSourceTokenCannotCoverAWholeFileReplace(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss1", WholeFile: true, Hashes: hashesFor("alpha", "beta", "gamma"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/a.go", "ss1", "alpha", "beta")

	if check := sourceTokenGate(a, "/w/a.go", "r_read1"); check.Satisfied {
		t.Fatal("a partial window must not authorize replacing the whole file")
	}
}

func TestSatisfiedSourceTokenBindsToTheOperation(t *testing.T) {
	writer := evidenceWriter{target: tool.EvidenceTargetInfo{
		Path: "/w/a.go", Snapshot: "ss1", WholeFile: true, Hashes: hashesFor("alpha", "beta"),
	}}
	a, ledger := newEvidenceAgent(t, writer, true)
	recordTokenObservation(ledger, "r_read1", "/w/a.go", "ss1", "alpha", "beta")
	sourceTokenGate(a, "/w/a.go", "r_read1")

	id := evidence.OperationID("write_file", []byte(`{"path":"/w/a.go","source_token":"r_read1"}`))
	op, ok := a.operations().Get(id)
	if !ok || len(op.SourceTokens) != 1 || op.SourceTokens[0] != "r_read1" {
		t.Fatalf("operation = %+v, want the source token recorded against it", op)
	}
}

func TestFileReadsPublishASourceTokenAndOtherReadsDoNot(t *testing.T) {
	if got := appendReceiptCitation("1→alpha", evidence.Receipt{ID: "r_1", Success: true, Read: true, ToolName: "read_file"}); got != "1→alpha\n[source_token r_1]" {
		t.Fatalf("read_file result = %q, want a source token trailer", got)
	}
	if got := appendReceiptCitation("a done", evidence.Receipt{ID: "r_2", Success: true, Read: true, ToolName: "list_dir"}); got != "a done" {
		t.Fatalf("a read with no versioned window must not carry a token: %q", got)
	}
}
