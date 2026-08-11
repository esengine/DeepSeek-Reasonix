package evidence

import (
	"encoding/json"
	"testing"
)

func TestSuccessfulProgressSignaturesUseObservedReadResult(t *testing.T) {
	first := ReceiptFromToolCall("grep", json.RawMessage(`{"path":"a.go","pattern":"TODO"}`), true, true)
	first.ObserveOutput("a.go:10: TODO")
	repeat := first
	changed := ReceiptFromToolCall("grep", json.RawMessage(`{"pattern":"TODO","path":"a.go"}`), true, true)
	changed.ObserveOutput("a.go:10: TODO\na.go:20: TODO")

	firstSig, ok := progressReceiptSignature(first)
	if !ok {
		t.Fatal("successful non-empty grep must produce progress evidence")
	}
	repeatSig, _ := progressReceiptSignature(repeat)
	changedSig, _ := progressReceiptSignature(changed)
	if firstSig != repeatSig {
		t.Fatalf("same args and result changed identity: %q != %q", firstSig, repeatSig)
	}
	if firstSig == changedSig {
		t.Fatal("same query with changed host-observed output must be new evidence")
	}
	if first.OutputBytes == 0 || first.OutputDigest == "" {
		t.Fatalf("ObserveOutput did not attest output: %+v", first)
	}
}
