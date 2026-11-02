package evidence

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestToolCallPathsReadsOnlyDeclaredPathFieldsTrimmedAndDistinct(t *testing.T) {
	args := json.RawMessage(`{"path":" a.go ","file_path":"a.go","notebook_path":"n.ipynb","source_path":"s","destination_path":"d","paths":["p1"," p2","p1",""],"file_paths":["f1"],"command":"rm -rf x","content":"not/a/path.go"}`)
	got := ToolCallPaths(args)
	want := []string{"a.go", "n.ipynb", "s", "d", "p1", "p2", "f1"}
	if !slices.Equal(got, want) {
		t.Fatalf("ToolCallPaths = %v, want %v", got, want)
	}
	for name, raw := range map[string]string{"not json": `{`, "a list": `["a.go"]`, "no paths": `{"command":"ls"}`, "non-string path": `{"path":5}`} {
		if got := ToolCallPaths(json.RawMessage(raw)); len(got) != 0 {
			t.Errorf("%s: ToolCallPaths = %v, want none (bash and unknown targets are not parsed)", name, got)
		}
	}
}

func TestTouchedPathsAreRecentFirstDistinctBoundedAndSayWhetherTheyWereWritten(t *testing.T) {
	l := recorded(
		Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"old.go"}},
		Receipt{ToolName: "edit_file", Success: true, Write: true, Paths: []string{"a.go", "b.go"}},
		Receipt{ToolName: "edit_file", Success: false, Write: true, Paths: []string{"failed.go"}},
		Receipt{ToolName: "bash", Success: true, Paths: []string{"not-a-read-or-write.go"}},
		Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"a.go", "c.go"}},
	)
	if got, want := l.TouchedPaths(10, false), []string{"a.go", "c.go", "b.go", "old.go"}; !slices.Equal(got, want) {
		t.Fatalf("touched = %v, want %v", got, want)
	}
	if got, want := l.TouchedPaths(10, true), []string{"a.go", "b.go"}; !slices.Equal(got, want) {
		t.Fatalf("written = %v, want %v", got, want)
	}
	if got := l.TouchedPaths(2, false); len(got) != 2 {
		t.Fatalf("limit ignored: %v", got)
	}
	var nilLedger *Ledger
	if nilLedger.TouchedPaths(5, false) != nil || l.TouchedPaths(0, false) != nil {
		t.Fatal("a nil ledger and a zero limit touch nothing")
	}
}
