package evidence

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLedgerRecordsSuccessAndFailureReceipts(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(Receipt{
		ToolName: "bash",
		Args:     json.RawMessage(`{"command":"go test ./..."}`),
		Success:  true,
		Command:  "go test ./...",
	})
	ledger.Record(Receipt{
		ToolName: "bash",
		Args:     json.RawMessage(`{"command":"go test ./internal/..."}`),
		Success:  false,
		Command:  "go test ./internal/...",
	})

	if !ledger.HasSuccessfulCommand("go test ./...") {
		t.Fatal("successful bash command should verify")
	}
	if ledger.HasSuccessfulCommand("go test ./internal/...") {
		t.Fatal("failed bash command must not verify")
	}
}

func TestLedgerMatchesFileReadAndWriteReceipts(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(Receipt{ToolName: "read_file", Success: true, Paths: []string{`internal/tool/builtin/completestep.go`}, Read: true})
	ledger.Record(Receipt{ToolName: "write_file", Success: true, Paths: []string{`internal/evidence/evidence.go`}, Write: true})
	ledger.Record(Receipt{ToolName: "edit_file", Success: false, Paths: []string{`failed.go`}, Write: true})

	if !ledger.HasSuccessfulReadOrWrite([]string{`internal\tool\builtin\completestep.go`}) {
		t.Fatal("read receipt should verify the same path across separators")
	}
	if !ledger.HasSuccessfulWrite([]string{`internal/evidence/evidence.go`}) {
		t.Fatal("write receipt should verify written path")
	}
	if ledger.HasSuccessfulWrite([]string{`failed.go`}) {
		t.Fatal("failed write receipt must not verify")
	}
}

func TestLedgerResetClearsTurnReceipts(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(Receipt{ToolName: "bash", Success: true, Command: "go test ./..."})

	ledger.Reset()

	if ledger.HasSuccessfulCommand("go test ./...") {
		t.Fatal("reset should clear prior-turn evidence")
	}
}

func TestContextCarriesLedger(t *testing.T) {
	ledger := NewLedger()
	ctx := WithLedger(context.Background(), ledger)

	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("ledger missing from context")
	}
	if got != ledger {
		t.Fatal("context returned a different ledger")
	}
}

func TestReceiptFromToolCallExtractsEvidenceFields(t *testing.T) {
	bash := ReceiptFromToolCall("bash", json.RawMessage(`{"command":"git diff --check"}`), true, false)
	if bash.Command != "git diff --check" {
		t.Fatalf("bash command = %q", bash.Command)
	}
	if bash.Write {
		t.Fatal("bash should not be treated as a verified file writer")
	}

	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"internal/evidence/evidence.go","content":"x"}`), true, false)
	if !write.Write || len(write.Paths) != 1 || write.Paths[0] != `internal/evidence/evidence.go` {
		t.Fatalf("write receipt not extracted: %+v", write)
	}

	read := ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/tool/builtin/completestep.go"}`), true, true)
	if !read.Read || len(read.Paths) != 1 {
		t.Fatalf("read receipt not extracted: %+v", read)
	}
}
