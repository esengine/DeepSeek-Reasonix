package agent

import (
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

func TestDecorateExecutionReceiptCarriesHostObservedOutcome(t *testing.T) {
	rec := evidence.Receipt{ToolName: "bash", Success: true}
	decorateExecutionReceipt(&rec, "  out  ", &tool.ShellExecution{
		ExitCode:     new(3),
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

func TestHostReceiptsAttestChangesAndVerifications(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"parser.go"}},
		{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"parser_test.go"}},
		{ToolName: "bash", Success: true, Command: "go test ./parser", ExitCode: new(0), Verification: evidence.VerificationPassed},
		{ToolName: "bash", Success: true, Command: "ls -la", ExitCode: new(0), Verification: evidence.VerificationNotVerification},
	}}

	got := formatHostReceipts(summary, writeclaim.WritePathSet{})
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

func TestHostReceiptsRecordFailedCommands(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "bash", Success: true, Command: "go build ./...", ExitCode: new(2)},
		{ToolName: "bash", Success: true, Command: "go test ./parser", ExitCode: new(1), Verification: evidence.VerificationFailed},
	}}
	got := formatHostReceipts(summary, writeclaim.WritePathSet{})
	for _, want := range []string{"go build ./... (exit 2)", "go test ./parser (verification failed, exit 1)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("receipts block %q missing %q", got, want)
		}
	}
}

func TestHostReceiptsStaySilentForReadOnlyChildren(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
		{ToolName: "read_file", Success: true, Read: true, Paths: []string{"parser.go"}},
		{ToolName: "grep", Success: true, Read: true},
	}}
	if got := formatHostReceipts(summary, writeclaim.WritePathSet{}); got != "" {
		t.Fatalf("read-only child produced a receipts block: %q", got)
	}
	if got := AppendHostReceipts("just prose", summary, writeclaim.WritePathSet{}); got != "just prose" {
		t.Fatalf("answer = %q, want it unchanged", got)
	}
}
