package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeleteRangeSameAnchorNonInclusiveNoDup probes the degenerate but valid
// call where start_anchor == end_anchor (one unique line) with inclusive=false.
// "delete nothing between a line and itself" must not corrupt the file; the
// overlap in the keep slices would otherwise duplicate that line.
func TestDeleteRangeSameAnchorNonInclusiveNoDup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("A\nB\nC\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"path":         path,
		"start_anchor": "B",
		"end_anchor":   "B",
		"inclusive":    false,
	})
	_, err := deleteRange{}.Execute(context.Background(), args)
	got, _ := os.ReadFile(path)
	if strings.Count(string(got), "B") > 1 {
		t.Fatalf("line B was duplicated — file corrupted: %q (err=%v)", string(got), err)
	}
}
