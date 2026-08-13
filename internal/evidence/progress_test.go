package evidence

import (
	"encoding/json"
	"testing"
)

func readReceipt(path string) Receipt {
	return ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"`+path+`"}`), true, true)
}

func bashReceipt(command string, success bool) Receipt {
	args, _ := json.Marshal(map[string]string{"command": command})
	return ReceiptFromToolCall("bash", args, success, false)
}

func TestProgressTrackerScoresNewVsRepeatedEvidence(t *testing.T) {
	tr := NewProgressTracker()

	first := readReceipt("a.go")
	first.OutputBytes = 10
	if got := tr.ScoreRound([]Receipt{first}); got != gainNewRead {
		t.Fatalf("new read gain = %d, want %d", got, gainNewRead)
	}
	repeat := readReceipt("a.go")
	repeat.OutputBytes = 10
	if got := tr.ScoreRound([]Receipt{repeat}); got != 0 {
		t.Fatalf("repeated read gain = %d, want 0", got)
	}

	if got := tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)}); got != gainNewFailure {
		t.Fatalf("first failure gain = %d, want %d (a new error localizes)", got, gainNewFailure)
	}
	if got := tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)}); got != gainRepeatFailure {
		t.Fatalf("same failure gain = %d, want %d", got, gainRepeatFailure)
	}
	if got := tr.ScoreRound([]Receipt{bashReceipt("go test ./x", true)}); got != gainStateChange {
		t.Fatalf("failure→pass gain = %d, want %d", got, gainStateChange)
	}
	if got := tr.ScoreRound([]Receipt{bashReceipt("go test ./x", true)}); got != 0 {
		t.Fatalf("repeated passing command gain = %d, want 0", got)
	}

	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"b.go","content":"x"}`), true, false)
	if got := tr.ScoreRound([]Receipt{write}); got != gainMutation {
		t.Fatalf("mutation gain = %d, want %d", got, gainMutation)
	}
}

func TestLedgerReceiptsSince(t *testing.T) {
	l := &Ledger{}
	l.Record(bashReceipt("one", true))
	l.Record(bashReceipt("two", true))
	if got := len(l.ReceiptsSince(1)); got != 1 {
		t.Fatalf("receipts since 1 = %d, want 1", got)
	}
	if got := l.ReceiptsSince(5); got != nil {
		t.Fatalf("out-of-range mark must yield nil, got %v", got)
	}
	var nilLedger *Ledger
	if got := nilLedger.ReceiptsSince(0); got != nil {
		t.Fatalf("nil ledger must yield nil")
	}
}
