package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

const beforeRevision = "package tally\n\nfunc Sum(xs []int) int {\n\ttotal := 0\n\treturn total\n}\n"
const afterRevision = "package tally\n\nfunc Sum(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\ttotal += x\n\t}\n\treturn total\n}\n"

// A diff carries the changed lines behind a +/- prefix, a pager or a line
// number, and none of that changes what the model read. Matching on substrings
// is what lets one rule cover every one of those shapes.
func TestOutputShowsLinesReadsThroughEveryPrefix(t *testing.T) {
	witness := ChangedLines(beforeRevision, afterRevision)
	if len(witness) == 0 {
		t.Fatal("a change with added lines produced no witness")
	}
	shown := []string{
		"diff --git a/tally.go b/tally.go\n@@ -3,5 +3,7 @@\n \ttotal := 0\n+\tfor _, x := range xs {\n+\t\ttotal += x\n+\t}\n \treturn total\n",
		"     5\t\tfor _, x := range xs {\n     6\t\t\ttotal += x\n     7\t\t}\n",
		afterRevision,
	}
	for _, output := range shown {
		if !OutputShowsLines(output, witness) {
			t.Errorf("output did not read as showing the change:\n%s", output)
		}
	}
	hidden := []string{
		" tally.go | 3 +++\n 1 file changed, 3 insertions(+)\n", // --stat
		"",
		"M tally.go\n",
		beforeRevision, // the revision the change replaced shows none of it
	}
	for _, output := range hidden {
		if OutputShowsLines(output, witness) {
			t.Errorf("output that carried no change content counted as review: %q", output)
		}
	}
}

// A change is proven by what it removed as much as by what it added, and the
// current file no longer holds the removed lines.
func TestChangedLinesCoverBothDirections(t *testing.T) {
	witness := ChangedLines("keep me\ndelete me\n", "keep me\n")
	if len(witness) != 1 || witness[0] != "delete me" {
		t.Fatalf("witness = %q, want the removed line", witness)
	}
	if OutputShowsLines("keep me\n", witness) {
		t.Error("a file that no longer holds the removed line did not show the change")
	}
	if !OutputShowsLines("-delete me\n", witness) {
		t.Error("a diff that printed the removed line showed the change")
	}
}

// A blank line occurs in every output. Counting it would make an unrelated
// command answer for a change that only added whitespace.
func TestWitnessDropsLinesThatDistinguishNothing(t *testing.T) {
	if witness := ChangedLines("a\n", "a\n\n\n"); len(witness) != 0 {
		t.Fatalf("witness = %q, want blank additions to prove nothing", witness)
	}
	if OutputShowsLines("anything at all", nil) {
		t.Error("an empty witness must never count as shown")
	}
}

// A witness is sampled across the whole change, so an output truncated to its
// beginning cannot answer for the end.
func TestWitnessSamplesTheWholeChange(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 1+i%7))
		b.WriteString(" number ")
		b.WriteString(strings.Repeat("9", 1+i%5))
		b.WriteByte('\n')
		b.WriteString("unique marker ")
		b.WriteString(strings.Repeat("z", 1+i%11))
		b.WriteString(strings.Repeat("q", 1+i/50))
		b.WriteByte('\n')
	}
	full := b.String()
	witness := ChangedLines("", full)
	if len(witness) > shownSampleLimit {
		t.Fatalf("witness = %d lines, want at most %d", len(witness), shownSampleLimit)
	}
	if !OutputShowsLines(full, witness) {
		t.Fatal("the whole file must show its own change")
	}
	head := full[:len(full)/8]
	if OutputShowsLines(head, witness) {
		t.Fatal("an output holding only the beginning answered for the whole change")
	}
}

func TestLedgerHostReviewCoverageRequiresContentForEveryPath(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/b.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("missing mutation index")
	}
	ledger.Record(ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{ReadOnly: true}))
	if ledger.HasHostReviewCoverageAfter(mutation, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatal("one path read must not cover a two-path change set")
	}
	ledger.Record(ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/b.go"}`), true, ToolFacts{ReadOnly: true}))
	if !ledger.HasHostReviewCoverageAfter(mutation, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatal("fresh reads of every changed path should prove host review coverage")
	}

	// What the shell command looked like decides nothing: coverage is what the
	// output carried, which is why a compound statement is no longer a wall.
	diff := NewLedger()
	diff.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	diff.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/b.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	diffMutation, ok := diff.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("missing diff mutation index")
	}
	diff.Record(Receipt{
		ToolName: "bash", Success: true, OutputBytes: 200,
		Command: "git status --short && git diff",
		Showed:  []string{"internal/a.go", "internal/b.go"},
	})
	if !diff.HasHostReviewCoverageAfter(diffMutation, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatal("an output that carried both changes should cover the change set")
	}

	summary := NewLedger()
	summary.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
	idx, ok := summary.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("missing summary mutation index")
	}
	summary.Record(Receipt{ToolName: "bash", Success: true, Command: "git diff --stat", OutputBytes: 200})
	if summary.HasHostReviewCoverageAfter(idx, []string{"internal/a.go"}) {
		t.Fatal("output that carried no change content must not count as review")
	}
}

// The review gate used to accept a command by name: `git status` printed the
// paths and counted, `echo internal/a.go` mentioned one and counted. Both show
// nothing of what changed, and both were how a turn could look reviewed.
func TestReviewOfAKnownChangeNeedsItsContent(t *testing.T) {
	changed := func() *Ledger {
		l := NewLedger()
		l.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/a.go"}`), true, ToolFacts{WritesNamedPaths: true}))
		return l
	}
	named := changed()
	named.Record(Receipt{ToolName: "bash", Success: true, Read: true, Command: "git status --short", OutputBytes: 40})
	if named.HasSuccessfulReviewAfter(mutationIndexOf(t, named)) {
		t.Error("a status listing counted as review of the change it only named")
	}

	mentioned := changed()
	mentioned.Record(Receipt{ToolName: "bash", Success: true, Read: true, Command: "echo internal/a.go", OutputBytes: 20})
	if mentioned.HasSuccessfulReviewAfter(mutationIndexOf(t, mentioned)) {
		t.Error("printing a path counted as review of the file at it")
	}

	shown := changed()
	shown.Record(Receipt{
		ToolName: "bash", Success: true, Command: "git status --short && git diff",
		OutputBytes: 400, Showed: []string{"internal/a.go"},
	})
	if !shown.HasSuccessfulReviewAfter(mutationIndexOf(t, shown)) {
		t.Error("an output that carried the change did not count as review of it")
	}
}

func mutationIndexOf(t *testing.T, l *Ledger) int {
	t.Helper()
	i, ok := l.LatestSuccessfulMutationIndex()
	if !ok {
		t.Fatal("missing mutation index")
	}
	return i
}
