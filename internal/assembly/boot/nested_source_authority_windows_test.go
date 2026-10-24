package boot

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNestedSourceProjectionRejectsAncestorJunctionEscape(t *testing.T) {
	source, candidate, outside := t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(source, "linked")
	if out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("create Windows junction: %v %s", err, out)
	}
	if _, err := mapIntegrationSourcePaths(candidate, source, []string{filepath.Join(candidate, "linked", "file.txt")}); err == nil {
		t.Fatal("nested source projection escaped through an ancestor junction")
	}
}
