package agent

import (
	"context"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/plancontract"
)

// The reply is model output, so it is parsed as untrusted input: anything that
// is not a line number in range is dropped rather than trusted. A miscounted or
// chatty answer must degrade to a smaller selection, never to a wrong one.
func TestParseDiagnosticLinesRejectsWhatItCannotVerify(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		total int
		want  []int
	}{
		{"plain list", "3,12,88", 100, []int{3, 12, 88}},
		{"range", "12-15", 100, []int{12, 13, 14, 15}},
		{"mixed with prose the prompt forbade", "Sure! Keep lines 3, 12-14 and 88.", 100, []int{3, 12, 13, 14, 88}},
		{"out of range dropped", "3,999,1000", 10, []int{3}},
		{"zero and negatives dropped", "0,-4,5", 10, []int{5}},
		{"duplicates collapse", "5,5,5,6", 10, []int{5, 6}},
		{"reversed range keeps only the start", "20-10", 100, []int{20}},
		{"empty reply", "", 100, nil},
		{"no numbers at all", "I could not determine the failure.", 100, nil},
	} {
		got := parseDiagnosticLines(tc.reply, tc.total)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}

// A runaway selection cannot make the projection grow: the ceiling that bounds
// the mechanical path bounds this one too.
func TestParseDiagnosticLinesRespectsTheCeiling(t *testing.T) {
	if got := parseDiagnosticLines("1-500", 500); len(got) != failureMaxKeptLines {
		t.Fatalf("selection length = %d, want the %d-line ceiling", len(got), failureMaxKeptLines)
	}
}

// The host takes the lines itself, so the content is always verbatim: a model
// that paraphrased or invented a line cannot get that text into the projection.
func TestKeepFromSelectionTakesLinesFromTheOriginal(t *testing.T) {
	lines := padded("line ", 40)
	lines[17] = "assertion failed: want 2.5, got 3"
	content := strings.Join(lines, "\n")

	got := keepFromSelection(content, []int{18})
	if !strings.Contains(got, "assertion failed: want 2.5, got 3") {
		t.Fatalf("selected line did not survive:\n%s", got)
	}
	if !strings.Contains(got, "lines omitted") {
		t.Error("no elision marker")
	}
	if strings.Contains(got, "line 020") {
		t.Errorf("unselected line leaked in:\n%s", got)
	}
}

// An empty or fully-covering selection means there is nothing to gain, so the
// content is returned untouched rather than rebuilt with a marker.
func TestKeepFromSelectionLeavesNothingToDoAlone(t *testing.T) {
	content := strings.Join(padded("line ", 30), "\n")
	if got := keepFromSelection(content, nil); got != content {
		t.Error("empty selection rewrote the content")
	}
	all := make([]int, 30)
	for i := range all {
		all[i] = i + 1
	}
	if got := keepFromSelection(content, all); got != content {
		t.Error("full selection rewrote the content")
	}
}

// Only long, protected failures without a recorded answer are worth a call.
func TestWantsDiagnosisSelectionAsksOnlyWhenItPays(t *testing.T) {
	code := 1
	long := strings.Join(padded("line ", 40), "\n")
	failed := func() *provider.ToolExecution {
		return &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &code}
	}
	for _, tc := range []struct {
		name string
		m    provider.Message
		want bool
	}{
		{"long protected failure", provider.Message{Content: long, ToolExecution: failed()}, true},
		{"successful execution", provider.Message{Content: long, ToolExecution: &provider.ToolExecution{State: tool.ShellStateCompleted}}, false},
		{"no execution record", provider.Message{Content: long}, false},
		{"short failure", provider.Message{Content: "boom", ToolExecution: failed()}, false},
		{"already shortened", provider.Message{Content: "… 4" + elisionMarker + "\n" + long, ToolExecution: failed()}, false},
		{"already answered", provider.Message{Content: long, ToolExecution: &provider.ToolExecution{
			State: tool.ShellStateFailed, ExitCode: &code, DiagnosticLines: []int{3}}}, false},
	} {
		if got := wantsDiagnosisSelection(tc.m); got != tc.want {
			t.Errorf("%s: wantsDiagnosisSelection = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A recorded selection is a model's reading of this exact output, so it wins
// over the word list — which is what it was introduced to replace.
func TestRecordedSelectionWinsOverTheWordList(t *testing.T) {
	code := 1
	lines := padded("[INFO] step ", 40)
	lines[10] = "note: the error handling code runs after the retry loop"
	lines[30] = "[ERR] handlers.go:88 unchecked return value"
	content := strings.Join(lines, "\n")

	a := &Agent{}
	m := provider.Message{Content: content, ToolExecution: &provider.ToolExecution{
		State: tool.ShellStateFailed, ExitCode: &code, DiagnosticLines: []int{31}}}
	got := a.window().keptForProjection(m)
	if !strings.Contains(got.Content, "[ERR] handlers.go:88") {
		t.Errorf("the selected failure line did not survive:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "the error handling code runs") {
		t.Errorf("the word list's stray hit overrode the recorded selection:\n%s", got.Content)
	}
}

// End to end over the real annotation pass: the model is asked once, the answer
// is recorded on the message, and a second pass costs no further call.
func TestAnnotateFailureDiagnosticsAsksOnceAndRecordsTheAnswer(t *testing.T) {
	code := 1
	lines := padded("[INFO] step ", 40)
	lines[30] = "[ERR] handlers.go:88 unchecked return value"
	content := strings.Join(lines, "\n")

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "29-32"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "1-5"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	region := []provider.Message{
		{Role: provider.RoleTool, Content: "fine", ToolExecution: &provider.ToolExecution{State: tool.ShellStateCompleted}},
		{Role: provider.RoleTool, Content: content, ToolExecution: &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &code}},
	}

	got := a.annotateFailureDiagnostics(context.Background(), region, nil)
	if len(prov.requests) != 1 {
		t.Fatalf("provider calls = %d, want exactly one (only the failure is worth asking about)", len(prov.requests))
	}
	sel := got[1].ToolExecution.DiagnosticLines
	if len(sel) != 4 || sel[0] != 29 {
		t.Fatalf("recorded selection = %v, want 29-32", sel)
	}
	if region[1].ToolExecution.DiagnosticLines != nil {
		t.Error("the caller's region was mutated in place")
	}
	// The recorded answer drives the shortening, and the failure survives it.
	shortened := a.window().keptForProjection(got[1])
	if !strings.Contains(shortened.Content, "[ERR] handlers.go:88") {
		t.Errorf("selected failure did not survive:\n%s", shortened.Content)
	}

	// Asking again about an already-answered message must not spend a call.
	if again := a.annotateFailureDiagnostics(context.Background(), got, nil); len(prov.requests) != 1 {
		t.Errorf("provider calls after re-annotation = %d, want no further call (%v)", len(prov.requests), again[1].ToolExecution.DiagnosticLines)
	}
}

// The model is optional infrastructure here: a refusal, a timeout, or an
// unparsable answer must leave the mechanical path in charge rather than lose
// the failure.
func TestAnnotateFailureDiagnosticsFallsBackWhenTheModelIsNoHelp(t *testing.T) {
	code := 1
	lines := padded("[INFO] step ", 40)
	lines[30] = "FAIL\tpkg/handlers\t0.4s"
	content := strings.Join(lines, "\n")
	failed := &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &code}

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "I cannot determine that."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	region := []provider.Message{{Role: provider.RoleTool, Content: content, ToolExecution: failed}}

	got := a.annotateFailureDiagnostics(context.Background(), region, nil)
	if len(got[0].ToolExecution.DiagnosticLines) != 0 {
		t.Fatalf("an unusable reply was recorded as a selection: %v", got[0].ToolExecution.DiagnosticLines)
	}
	shortened := a.window().keptForProjection(got[0])
	if shortened.Content == content {
		t.Error("nothing was shortened: the mechanical fallback did not run")
	}
	if !strings.Contains(shortened.Content, "FAIL\tpkg/handlers") {
		t.Errorf("the verdict did not survive the fallback:\n%s", shortened.Content)
	}
}

// Whether a handoff owes another attempt is read off the plan contract, not off
// the executor's wording. The caller has already established that no tool ran;
// what remains is whether the plan called for one, which its steps state.
func TestHandoffNudgeReadsThePlanContract(t *testing.T) {
	action := plancontract.Plan{Objective: "fix it", Steps: []plancontract.Step{
		{Title: "edit the parser", CandidateFiles: []string{"parser.go"}}}}
	verify := plancontract.Plan{Objective: "check it", Steps: []plancontract.Step{
		{Title: "run the suite", Verification: []plancontract.Verification{{Command: "go test ./..."}}}}}
	prose := plancontract.Plan{Objective: "explain it", Steps: []plancontract.Step{
		{Title: "describe how the retry budget works"}}}

	for _, tc := range []struct {
		name string
		plan *plancontract.Plan
		want bool
	}{
		{"no contract trusts the answer", nil, false},
		{"plan naming files expects action", &action, true},
		{"plan naming a command expects action", &verify, true},
		{"prose-only plan does not", &prose, false},
	} {
		a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
		a.SetPlanContract(tc.plan)
		if got := a.shouldNudgeExecutorHandoff(); got != tc.want {
			t.Errorf("%s: shouldNudgeExecutorHandoff = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Oversize output is only truncated when there is nowhere to put it. An agent
// with a transcript spills beside it; a sub-agent, which has none, spills into
// the workspace — losing the middle of a long result is not the price of being
// a sub-agent.
func TestOversizeOutputSpillsRatherThanTruncates(t *testing.T) {
	big := strings.Repeat("a line of tool output padded out a bit\n", 2000)
	root := testenv.TempDir(t)

	withRoot := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: root}, event.Discard)
	out, _, notice := withRoot.boundToolOutput(big, "bash", "call-1", "", false)
	if notice != "" {
		t.Errorf("spilled output must carry no truncation notice: %q", notice)
	}
	if !strings.Contains(out, "kept out of context") || !strings.Contains(out, filepath.Join(root, "outputs")) {
		t.Fatalf("expected a spill pointer into the workspace:\n%s", out[:min(240, len(out))])
	}
	spilled, err := os.ReadFile(filepath.Join(root, "outputs", "call-1.txt"))
	if err != nil || len(spilled) != len(big) {
		t.Fatalf("spill file: %d bytes, err=%v; want the full %d", len(spilled), err, len(big))
	}

	// Owning neither a transcript nor an archive is likewise not a reason to
	// lose the middle: scratch is somewhere, and truncation drops most of a
	// result this size.
	bare := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	bareOut, _, bareNotice := bare.boundToolOutput(big, "bash", "call-2", "", false)
	if bareNotice != "" {
		t.Errorf("the scratch fallback must keep the result whole, not truncate it: %q", bareNotice)
	}
	scratch := spillPathIn(t, bareOut)
	t.Cleanup(func() { _ = os.Remove(scratch) })
	if kept, err := os.ReadFile(scratch); err != nil || len(kept) != len(big) {
		t.Fatalf("scratch spill: %d bytes, err=%v; want the full %d", len(kept), err, len(big))
	}
}
