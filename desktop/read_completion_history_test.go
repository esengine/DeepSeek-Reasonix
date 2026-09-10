package main

import (
	"reasonix/internal/provider"
	"testing"
)

func TestReadCompletionHistoryRowSurvivesProjection(t *testing.T) {
	r := &provider.ReadCompletion{ID: "run", Reads: []provider.CompletedRead{{Path: "fixture.txt", Verdict: "partial_read_sufficient", Covered: [][2]int{{0, 12}}}}}
	rows, ok := historyLocalOnlyRows(provider.Message{LocalOnly: true, ReadCompletion: r})
	if !ok || len(rows) != 1 || rows[0].Code != "read_completion" || rows[0].ReadCompletion != r {
		t.Fatalf("rows=%+v ok=%v", rows, ok)
	}
}
