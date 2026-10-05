package builtin

import (
	"context"
	"testing"

	"reasonix/internal/contract/tool"
)

// needsItsOwnPlace are the built-ins that touch no file and still cannot share
// a batch, with what makes each one so. ReadOnly answers approval, not
// batching, so every one of these has to declare Sequential itself — and one
// that quietly stops declaring it starts running beside the calls it has to
// follow, which the batch planner has no other way to know.
var needsItsOwnPlace = map[string]string{
	"complete_step": "signing a step off advances the task list",
	"todo_write":    "each write replaces the whole list",
	"compress":      "a fold rewrites the transcript the rest of the batch reads",
	"wait":          "it waits on jobs started earlier in the same reply",
	"bash_output":   "it reads what an earlier call in the same reply started",
}

func TestReadOnlyToolsThatStillNeedTheirOwnPlace(t *testing.T) {
	found := map[string]bool{}
	for _, x := range tool.Builtins() {
		why, listed := needsItsOwnPlace[x.Name()]
		if !listed {
			continue
		}
		found[x.Name()] = true
		if !x.ReadOnly() {
			// A writer is already serial; being on this list would then prove
			// nothing, and the entry should go rather than pass vacuously.
			t.Errorf("%s is no longer ReadOnly, so this list no longer says anything about it", x.Name())
		}
		if !tool.RunsSequentially(context.Background(), x, nil) {
			t.Errorf("%s stopped declaring Sequential: it would now share a parallel batch, but %s", x.Name(), why)
		}
	}
	for name := range needsItsOwnPlace {
		if !found[name] {
			t.Errorf("%s is not a registered built-in; the list is describing a tool that no longer exists", name)
		}
	}
}

// The other half: an ordinary reader must not pick the declaration up, or the
// batching this contract exists to allow quietly stops happening.
func TestOrdinaryReadersStayParallelisable(t *testing.T) {
	readers := []string{"read_file", "grep", "recall"}
	seen := 0
	for _, x := range tool.Builtins() {
		for _, name := range readers {
			if x.Name() != name {
				continue
			}
			seen++
			if !x.ReadOnly() {
				t.Fatalf("%s is not ReadOnly; pick a different reader for this test", name)
			}
			if tool.RunsSequentially(context.Background(), x, nil) {
				t.Errorf("%s declares Sequential, so it no longer shares a batch with other reads", name)
			}
		}
	}
	if seen != len(readers) {
		t.Fatalf("found %d of %d readers; the registry is not populated here", seen, len(readers))
	}
}
