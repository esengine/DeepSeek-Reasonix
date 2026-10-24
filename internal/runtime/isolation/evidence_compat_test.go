package isolation

import (
	"encoding/json"
	"testing"
)

func TestAppendingCandidateReceiptsPreservesUnknownEvidenceFields(t *testing.T) {
	previous := json.RawMessage(`{"environment_id":"iso_12345678","workspace":"/candidate","verification":"unknown","future_evidence":{"retained":true},"receipts":[{"tool":"write_file","success":true,"output_digest":"a","future_receipt":{"retained":true}}]}`)
	var before CandidateEvidence
	if err := json.Unmarshal(previous, &before); err != nil {
		t.Fatal(err)
	}
	after := before
	after.Receipts = append(append([]CandidateReceipt(nil), before.Receipts...), CandidateReceipt{Tool: "bash", Success: true, OutputDigest: "b"})
	next, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeEvidenceFields(previous, next, &before, &after)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(merged, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["future_evidence"].(map[string]any)["retained"] != true {
		t.Fatal("unknown evidence field lost")
	}
	receipts := fields["receipts"].([]any)
	if len(receipts) != 2 || receipts[0].(map[string]any)["future_receipt"].(map[string]any)["retained"] != true {
		t.Fatal("unknown receipt fields lost while appending")
	}
}
