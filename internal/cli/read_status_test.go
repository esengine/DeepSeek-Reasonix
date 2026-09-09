package cli

import (
	"reasonix/internal/event"
	"strings"
	"testing"
)

func TestReadStatusUsesDisjointOneBasedRanges(t *testing.T) {
	got := readStatusLabelText(&event.ReadStatusPayload{ReadID: "r", Path: "a.go", Active: true, HasMore: true, Covered: [][2]int{{0, 10}, {100, 110}}})
	if !strings.Contains(got, "1-10, 101-110") {
		t.Fatalf("false continuous coverage: %s", got)
	}
}

func TestReadStatusFencesGenerationsAndIndependentReads(t *testing.T) {
	var state readStatusState
	state.ingest(&event.ReadStatusPayload{ReadID: "a", Path: "a.go", Generation: 2, Sequence: 1, Active: true})
	state.ingest(&event.ReadStatusPayload{ReadID: "a", Path: "a.go", Generation: 1, Sequence: 99, Active: false})
	if !strings.Contains(state.readStatusLabel, "a.go") {
		t.Fatal("stale frame cleared current state")
	}
	state.ingest(&event.ReadStatusPayload{ReadID: "b", Path: "b.go", Sequence: 1, Active: true})
	state.ingest(&event.ReadStatusPayload{ReadID: "a", Path: "a.go", Generation: 2, Sequence: 2, Active: false})
	if !strings.Contains(state.readStatusLabel, "b.go") {
		t.Fatal("independent read disappeared")
	}
}
