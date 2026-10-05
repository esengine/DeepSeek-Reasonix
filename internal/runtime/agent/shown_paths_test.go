package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

const witnessBefore = "package tally\n\nfunc Sum(xs []int) int {\n\ttotal := 0\n\treturn total\n}\n"
const witnessAfter = "package tally\n\nfunc Sum(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\ttotal += x\n\t}\n\treturn total\n}\n"

func writeReceipt(path string) evidence.Receipt {
	return evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven, Paths: []string{path},
	}
}

// The turn's own diff is what a run shows itself before finishing, and it
// arrives in whatever shell wrapper the model happened to type. The wrapper is
// exactly what the host used to have to read; now it reads the output.
func TestACompoundDiffCoversTheChangeItPrinted(t *testing.T) {
	a := &Agent{}
	write := writeReceipt("tally.go")
	a.reviewCoverageOf(&write, &toolCallPlan{
		mutationPath:    "tally.go",
		mutationWitness: evidence.ChangedLines(witnessBefore, witnessAfter),
	}, "wrote 132 bytes to tally.go")
	if len(write.Showed) != 0 {
		t.Fatalf("the write showed %q; a call that changed a file cannot review it", write.Showed)
	}

	review := evidence.Receipt{ToolName: "bash", Success: true, Command: "git status --short && git diff"}
	a.reviewCoverageOf(&review, &toolCallPlan{}, "M tally.go\n@@ -3,5 +3,7 @@\n \ttotal := 0\n+\tfor _, x := range xs {\n+\t\ttotal += x\n+\t}\n \treturn total\n")
	if !slices.Contains(review.Showed, "tally.go") {
		t.Fatalf("showed = %q, want the path whose change the output carried", review.Showed)
	}

	summary := evidence.Receipt{ToolName: "bash", Success: true, Command: "git diff --stat"}
	a.reviewCoverageOf(&summary, &toolCallPlan{}, " tally.go | 3 +++\n 1 file changed\n")
	if len(summary.Showed) != 0 {
		t.Fatalf("showed = %q, want a summary to carry no change content", summary.Showed)
	}
}

// A shell write names no earlier revision, so the host cannot know which lines
// changed. Showing such a file means showing the file.
func TestAShellWrittenFileIsWitnessedByItsContent(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(path, []byte(witnessAfter), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{}
	shell := evidence.Receipt{
		ToolName: "bash", Success: true, Mutation: true,
		Command: "printf '%s' ... > probe.go", Paths: []string{path},
	}
	a.reviewCoverageOf(&shell, &toolCallPlan{}, "")
	if len(a.task.witness[path]) == 0 {
		t.Fatal("a shell write with a known path recorded no witness")
	}

	read := evidence.Receipt{ToolName: "bash", Success: true, Command: "cat probe.go"}
	a.reviewCoverageOf(&read, &toolCallPlan{}, witnessAfter)
	if !slices.Contains(read.Showed, path) {
		t.Fatalf("showed = %q, want the file the output printed", read.Showed)
	}

	partial := evidence.Receipt{ToolName: "bash", Success: true, Command: "head -2 probe.go"}
	a.reviewCoverageOf(&partial, &toolCallPlan{}, "package tally\n")
	if len(partial.Showed) != 0 {
		t.Fatalf("showed = %q, want part of a file not to answer for all of it", partial.Showed)
	}
}
