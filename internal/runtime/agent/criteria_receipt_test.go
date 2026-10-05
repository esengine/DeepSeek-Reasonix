package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// previewingEdit is a writer that describes its change the way the file-editing
// built-ins do, which is where the host reads a rewritten check from.
type previewingEdit struct{ change diff.Change }

func (previewingEdit) Name() string            { return "edit_file" }
func (previewingEdit) Description() string     { return "" }
func (previewingEdit) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (previewingEdit) ReadOnly() bool          { return false }
func (previewingEdit) WritesNamedPaths() bool  { return true }
func (p previewingEdit) Preview(context.Context, json.RawMessage) (diff.Change, error) {
	return p.change, nil
}
func (previewingEdit) Execute(context.Context, json.RawMessage) (string, error) { return "edited", nil }

const criteriaTestBefore = `package m

import "testing"

func TestMedian(t *testing.T) {
	if got := Median([]int{3, 1, 2}); got != 2 {
		t.Fatalf("Median = %v", got)
	}
}
`

const criteriaTestAfter = `package m

import "testing"

func TestMedian(t *testing.T) {
	if got := Median([]int{3, 1, 2}); got != 1 {
		t.Fatalf("Median = %v", got)
	}
}
`

// The turn that edits its own check must carry that fact all the way to the
// ledger: an impossible requirement met by moving the expectation leaves a
// green suite behind, and the pass count alone cannot tell the two apart.
func TestRewrittenCheckReachesTheLedger(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(previewingEdit{change: diff.Build("mathutil/mathutil_test.go", criteriaTestBefore, criteriaTestAfter, diff.Modify)})
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "edit_file", Arguments: `{"path":"mathutil/mathutil_test.go"}`,
	})
	if strings.HasPrefix(out.output, "blocked:") || out.errMsg != "" {
		t.Fatalf("edit did not run: %+v", out)
	}
	got := a.task.ledger.RewrittenCriteria()
	if len(got) != 1 || got[0] != "TestMedian" {
		t.Fatalf("ledger rewritten criteria = %v, want [TestMedian]", got)
	}
}

// Adding a case is how a fix lands. Reporting it would train the reader to
// ignore the field.
func TestAddedCheckIsNotReported(t *testing.T) {
	added := criteriaTestBefore + `
func TestMedianEven(t *testing.T) {
	if got := Median([]int{1, 2, 3, 4}); got != 2.5 {
		t.Fatalf("Median = %v", got)
	}
}
`
	reg := tool.NewRegistry()
	reg.Add(previewingEdit{change: diff.Build("mathutil/mathutil_test.go", criteriaTestBefore, added, diff.Modify)})
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "edit_file", Arguments: `{"path":"mathutil/mathutil_test.go"}`,
	})
	if got := a.task.ledger.RewrittenCriteria(); len(got) != 0 {
		t.Fatalf("adding a test reported %v, want none", got)
	}
}

// A rewritten pytest case reaches the ledger like a Go one, but is not held:
// nothing re-runs a captured Python criterion, so holding it would owe the
// turn a baseline run no backend can settle.
func TestRewrittenPytestCaseIsReportedNotHeld(t *testing.T) {
	before := "def test_median():\n    assert median([3, 1, 2]) == 2\n"
	after := "def test_median():\n    assert median([3, 1, 2]) == 1\n"
	reg := tool.NewRegistry()
	reg.Add(previewingEdit{change: diff.Build("tests/test_calc.py", before, after, diff.Modify)})
	a := New(nil, reg, sessionstore.NewSession(""), Options{ArchiveDir: t.TempDir()}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "edit_file", Arguments: `{"path":"tests/test_calc.py"}`,
	})
	if strings.HasPrefix(out.output, "blocked:") || out.errMsg != "" {
		t.Fatalf("edit did not run: %+v", out)
	}
	if got := a.task.ledger.RewrittenCriteria(); len(got) != 1 || got[0] != "test_median" {
		t.Fatalf("ledger rewritten criteria = %v, want [test_median]", got)
	}
	if len(a.task.baselineCriteria) != 0 {
		t.Fatalf("a pytest criterion was held: %v", a.task.baselineCriteria)
	}
}
