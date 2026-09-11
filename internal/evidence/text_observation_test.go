package evidence

import "testing"

func TestLedgerTextObservationUsesSharedSequenceAndBoundary(t *testing.T) {
	l := NewLedger()
	boundary := l.ObservationBoundary()
	l.Record(Receipt{ToolName: "read_file", Success: true, Read: true})
	l.RecordTextObservation(TextObservation{Path: "/tmp/a.go", StartLine: 1, LineHashes: []string{"a"}})
	observations := l.TextObservations()
	if len(observations) != 1 || observations[0].Sequence <= boundary {
		t.Fatalf("observations = %+v, boundary = %d", observations, boundary)
	}
	if observations[0].Sequence <= 1 {
		t.Fatalf("observation sequence = %d, want after receipt sequence 1", observations[0].Sequence)
	}
	l.Reset()
	if got := l.TextObservations(); len(got) != 0 {
		t.Fatalf("Reset left observations: %+v", got)
	}
}
