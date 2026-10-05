package evidence

import (
	"runtime"
	"testing"
)

// Created is stored under the ledger's path identity and asked in whatever
// spelling the caller happens to hold, so the two have to meet there. Missing
// each other turns "the turn made this file" into "the turn deleted one it
// found" — the difference between a cleanup and a change.
func TestCreatedInTurnIgnoresSpelling(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Mutation: true, Created: []string{"internal/New.go"}})

	for _, ask := range []string{"internal/New.go", `internal\New.go`, "./internal/New.go"} {
		if !l.CreatedInTurn(ask) {
			t.Errorf("CreatedInTurn(%q) = false, want true", ask)
		}
	}
	if runtime.GOOS == "windows" && !l.CreatedInTurn("internal/new.go") {
		t.Error(`CreatedInTurn("internal/new.go") = false, but Windows resolves it to the same file`)
	}
	if l.CreatedInTurn("internal/other.go") {
		t.Error("a path the turn never created counted as created")
	}
	if l.CreatedInTurn("") {
		t.Error("an empty path counted as created")
	}
}
