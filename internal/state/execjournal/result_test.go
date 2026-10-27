package execjournal

import (
	"bytes"
	"os"
	"path/filepath"
	"reasonix/internal/state/store"
	"testing"
)

func TestResultSnapshotSurvivesRepeatedSettlementWithoutRewritingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent.jsonl")
	if err := Open(path, Opening{ID: "run", Kind: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := Start(path, "run"); err != nil {
		t.Fatal(err)
	}
	Settle(path, "run", &Result{Ref: "sa_saved", State: "completed", ContextRefs: []string{"sa_source"}})
	before, err := os.ReadFile(store.SessionExecution(path))
	if err != nil {
		t.Fatal(err)
	}
	Settle(path, "run")
	after, err := os.ReadFile(store.SessionExecution(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("append rewrote the result snapshot")
	}
	entries := History(path)
	if len(entries) != 1 || entries[0].Result == nil || entries[0].Result.Ref != "sa_saved" || entries[0].Result.State != "completed" {
		t.Fatalf("lost terminal result: %+v", entries)
	}
}
