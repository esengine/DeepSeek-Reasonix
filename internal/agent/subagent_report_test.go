package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func intPtr(v int) *int { return &v }

func TestHostReceiptsAttestChangesAndVerifications(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"parser.go"}},
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"parser_test.go"}},
		{ToolName: "bash", Success: true, Command: "go test ./parser", ExitCode: intPtr(0), Verification: evidence.VerificationPassed},
		{ToolName: "bash", Success: true, Command: "ls -la", ExitCode: intPtr(0), Verification: evidence.VerificationNotVerification},
	}}

	got := formatHostReceipts(summary, WritePathSet{})
	for _, want := range []string{"changed: parser.go, parser_test.go", "go test ./parser (verification passed, exit 0)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("receipts block %q missing %q", got, want)
		}
	}
	// A plain read command is not a claim the parent must adjudicate, so it
	// never spends parent context.
	if strings.Contains(got, "ls -la") {
		t.Fatalf("receipts block must not list non-verification commands: %q", got)
	}
}

func TestHostReceiptsStaySilentForReadOnlyChildren(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "read_file", Success: true, Read: true, Paths: []string{"parser.go"}},
		{ToolName: "grep", Success: true, Read: true},
	}}
	if got := formatHostReceipts(summary, WritePathSet{}); got != "" {
		t.Fatalf("read-only child produced a receipts block: %q", got)
	}
	if got := appendHostReceipts("just prose", summary, WritePathSet{}); got != "just prose" {
		t.Fatalf("answer = %q, want it unchanged", got)
	}
}

func TestHostReceiptsRecordFailedCommands(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "bash", Success: true, Command: "go build ./...", ExitCode: intPtr(2)},
		{ToolName: "bash", Success: true, Command: "go test ./parser", ExitCode: intPtr(1), Verification: evidence.VerificationFailed},
	}}
	got := formatHostReceipts(summary, WritePathSet{})
	for _, want := range []string{"go build ./... (exit 2)", "go test ./parser (verification failed, exit 1)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("receipts block %q missing %q", got, want)
		}
	}
}

func TestDecorateExecutionReceiptCarriesHostObservedOutcome(t *testing.T) {
	rec := evidence.Receipt{ToolName: "bash", Success: true}
	decorateExecutionReceipt(&rec, "  out  ", &tool.ShellExecution{
		ExitCode:     tool.IntPtr(3),
		Verification: tool.ShellVerificationFailed,
	})
	if rec.ExitCode == nil || *rec.ExitCode != 3 {
		t.Fatalf("ExitCode = %v, want 3", rec.ExitCode)
	}
	if rec.Verification != evidence.VerificationFailed {
		t.Fatalf("Verification = %q, want %q", rec.Verification, evidence.VerificationFailed)
	}
	if rec.OutputBytes != len("out") {
		t.Fatalf("OutputBytes = %d, want %d", rec.OutputBytes, len("out"))
	}
	// A tool that ran no process must not gain a fabricated exit status.
	plain := evidence.Receipt{ToolName: "read_file", Success: true}
	decorateExecutionReceipt(&plain, "body", nil)
	if plain.ExitCode != nil {
		t.Fatalf("non-shell receipt gained ExitCode %v", plain.ExitCode)
	}
}

type fakeWriteFileTool struct{}

func (fakeWriteFileTool) Name() string        { return "write_file" }
func (fakeWriteFileTool) Description() string { return "Write a file." }
func (fakeWriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (fakeWriteFileTool) ReadOnly() bool { return false }
func (fakeWriteFileTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "written", nil
}

// The parent must learn what the child actually changed even when the child's
// own prose says nothing about it.
func TestSubAgentAnswerCarriesHostReceipts(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeWriteFileTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "write_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "all done"}, {Type: provider.ChunkDone}},
	}}

	answer, err := RunSubAgentWithSession(context.Background(), prov, reg, NewSession("sys"),
		"fix the parser", Options{}, event.Discard)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if !strings.Contains(answer, "all done") {
		t.Fatalf("answer lost the child's own summary: %q", answer)
	}
	if !strings.Contains(answer, hostReceiptsHeader) || !strings.Contains(answer, "parser.go") {
		t.Fatalf("answer missing host receipts for the write it performed: %q", answer)
	}
}

func TestSplitHostReceiptsSeparatesProseFromAttestation(t *testing.T) {
	answer := appendHostReceipts("did the thing",
		evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"a.go"}},
		}}, WritePathSet{})
	prose, receipts := splitHostReceipts(answer)
	if prose != "did the thing" {
		t.Fatalf("prose = %q", prose)
	}
	if !strings.HasPrefix(receipts, hostReceiptsHeader) || !strings.Contains(receipts, "a.go") {
		t.Fatalf("receipts = %q", receipts)
	}
	if p, r := splitHostReceipts("plain answer"); p != "plain answer" || r != "" {
		t.Fatalf("plain answer split to %q / %q", p, r)
	}
}

// A verbose child must not be able to push the host's attestation out of a
// fleet aggregate by writing a long answer.
func TestAggregateReservesHostReceiptsAgainstLongProse(t *testing.T) {
	answer := appendHostReceipts(strings.Repeat("chatter. ", 8000),
		evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"payments.go"}},
			{ToolName: "bash", Success: true, Command: "go test ./pay", ExitCode: intPtr(1), Verification: evidence.VerificationFailed},
		}}, WritePathSet{})

	out := formatBoundedSubagentAggregate("fleet:\n", []subagentAggregateItem{
		{header: "1. writer\n", status: "completed\n", answer: answer, ref: "sa_1"},
	})
	if !strings.Contains(out, "preview truncated") {
		t.Fatal("expected the prose to be truncated in this fixture")
	}
	for _, want := range []string{hostReceiptsHeader, "payments.go", "go test ./pay (verification failed, exit 1)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("aggregate dropped %q from the host attestation:\n%s", want, out)
		}
	}
}

// When attestations alone would starve the budget they lose detail, never the
// fact that a write escaped the declared claim.
func TestAggregateDegradesReceiptsButKeepsViolations(t *testing.T) {
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]subagentAggregateItem, 0, 64)
	for i := range 64 {
		summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(root, "auth", strings.Repeat("deep/", 20)+"f.go")}},
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{filepath.Join(root, strings.Repeat("out/", 20)+"escaped.go")}},
		}}
		items = append(items, subagentAggregateItem{
			header: fmt.Sprintf("%d. writer\n", i+1),
			status: "completed\n",
			answer: appendHostReceipts("done", summary, claim),
		})
	}

	out := formatBoundedSubagentAggregate("fleet:\n", items)
	if n := strings.Count(out, hostReceiptsViolationLabel); n != len(items) {
		t.Fatalf("violation lines = %d, want %d — a claim escape was dropped to save space", n, len(items))
	}
	if len(out) > 32*1024 {
		t.Fatalf("aggregate = %d bytes, over the tool output budget", len(out))
	}
}
