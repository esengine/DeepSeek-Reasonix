package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRecordDismissedTodoBatchRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordDismissedTodoBatch(path, `[{"content":"Ship"}]`); err != nil {
		t.Fatal(err)
	}
	if err := RecordDismissedTodoBatch(path, `[{"content":"Ship"}]`); err != nil {
		t.Fatal(err)
	}
	if err := RecordDismissedTodoBatch(path, `[{"content":"Next"}]`); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v ok=%v err=%v", got, ok, err)
	}
	if len(got.DismissedTodoBatches) != 2 || got.DismissedTodoBatches[0] != `[{"content":"Ship"}]` || got.DismissedTodoBatches[1] != `[{"content":"Next"}]` {
		t.Fatalf("dismissed batches = %#v", got.DismissedTodoBatches)
	}
}

func TestSaveBranchMetaPreservesDismissedTodoBatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordDismissedTodoBatch(path, "batch-a"); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMetaPreserveUpdated(path, BranchMeta{ID: "session", Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v ok=%v err=%v", got, ok, err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name = %q", got.Name)
	}
	if len(got.DismissedTodoBatches) != 1 || got.DismissedTodoBatches[0] != "batch-a" {
		t.Fatalf("dismissed batches dropped: %#v", got.DismissedTodoBatches)
	}
}

func TestOldBranchMetaWithoutDismissedBatchesStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("{\n  \"id\": \"session\",\n  \"name\": \"legacy\"\n}\n")
	if err := os.WriteFile(BranchMetaPath(path), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v ok=%v err=%v", got, ok, err)
	}
	if got.Name != "legacy" || len(got.DismissedTodoBatches) != 0 {
		t.Fatalf("legacy meta = %+v", got)
	}
}

func TestDismissedTodoBatchesOmitemptyOnEmpty(t *testing.T) {
	raw, err := json.Marshal(BranchMeta{ID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "dismissed_todo_batches") {
		t.Fatalf("empty dismissed batches should omit: %s", raw)
	}
}

func TestRecordDismissedTodoBatchSkipsMissingSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")
	if err := RecordDismissedTodoBatch(path, "batch-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BranchMetaPath(path)); !os.IsNotExist(err) {
		t.Fatalf("missing session grew a sidecar: %v", err)
	}
}

func TestNormalizeDismissedTodoBatchesCapsAndDedups(t *testing.T) {
	keys := []string{" keep ", "keep", ""}
	for i := range maxDismissedTodoBatches + 2 {
		keys = append(keys, "batch-"+strconv.Itoa(i))
	}
	got := NormalizeDismissedTodoBatches(keys)
	if len(got) != maxDismissedTodoBatches {
		t.Fatalf("len = %d, want %d", len(got), maxDismissedTodoBatches)
	}
	if got[0] == "keep" || got[len(got)-1] != "batch-"+strconv.Itoa(maxDismissedTodoBatches+1) {
		t.Fatalf("cap should keep the newest fingerprints: first=%q last=%q", got[0], got[len(got)-1])
	}
}
