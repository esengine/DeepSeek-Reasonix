package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type fakeWriteFileTool struct{}

func (fakeWriteFileTool) Name() string        { return "write_file" }
func (fakeWriteFileTool) Description() string { return "Write a file." }
func (fakeWriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (fakeWriteFileTool) ReadOnly() bool         { return false }
func (fakeWriteFileTool) WritesNamedPaths() bool { return true }
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

	answer, err := agent.RunSubAgentWithSession(context.Background(), prov, reg, sessionstore.NewSession("sys"),
		"fix the parser", agent.Options{}, event.Discard)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if !strings.Contains(answer, "all done") {
		t.Fatalf("answer lost the child's own summary: %q", answer)
	}
	if !strings.Contains(answer, agent.HostReceiptsHeader) || !strings.Contains(answer, "parser.go") {
		t.Fatalf("answer missing host receipts for the write it performed: %q", answer)
	}
}

// When attestations alone would starve the budget they lose detail, never the
// fact that a write escaped the declared claim.
func TestAggregateDegradesReceiptsButKeepsViolations(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"auth"})
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
			answer: agent.AppendHostReceipts("done", summary, claim),
		})
	}

	out := formatBoundedSubagentAggregate("fleet:\n", items)
	if n := strings.Count(out, agent.HostReceiptsViolationLabel); n != len(items) {
		t.Fatalf("violation lines = %d, want %d — a claim escape was dropped to save space", n, len(items))
	}
	if len(out) > 32*1024 {
		t.Fatalf("aggregate = %d bytes, over the tool output budget", len(out))
	}
}

// A verbose child must not be able to push the host's attestation out of a
// fleet aggregate by writing a long answer.
func TestAggregateReservesHostReceiptsAgainstLongProse(t *testing.T) {
	answer := agent.AppendHostReceipts(strings.Repeat("chatter. ", 8000),
		evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"payments.go"}},
			{ToolName: "bash", Success: true, Command: "go test ./pay", ExitCode: new(1), Verification: evidence.VerificationFailed},
		}}, writeclaim.WritePathSet{})

	out := formatBoundedSubagentAggregate("fleet:\n", []subagentAggregateItem{
		{header: "1. writer\n", status: "completed\n", answer: answer, ref: "sa_1"},
	})
	if !strings.Contains(out, "preview truncated") {
		t.Fatal("expected the prose to be truncated in this fixture")
	}
	for _, want := range []string{agent.HostReceiptsHeader, "payments.go", "go test ./pay (verification failed, exit 1)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("aggregate dropped %q from the host attestation:\n%s", want, out)
		}
	}
}

func TestSplitHostReceiptsSeparatesProseFromAttestation(t *testing.T) {
	answer := agent.AppendHostReceipts("did the thing",
		evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"a.go"}},
		}}, writeclaim.WritePathSet{})
	prose, receipts := splitHostReceipts(answer)
	if prose != "did the thing" {
		t.Fatalf("prose = %q", prose)
	}
	if !strings.HasPrefix(receipts, agent.HostReceiptsHeader) || !strings.Contains(receipts, "a.go") {
		t.Fatalf("receipts = %q", receipts)
	}
	if p, r := splitHostReceipts("plain answer"); p != "plain answer" || r != "" {
		t.Fatalf("plain answer split to %q / %q", p, r)
	}
}
