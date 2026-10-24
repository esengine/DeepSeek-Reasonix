package mutation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeldPathsCoverOnlyTheSameFileAcrossCaseSpellings(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	lease, err := c.Acquire(t.Context(), Intent{Effect: NamedPaths, Paths: []string{"absent.go", "existing.go"}})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(NotExecuted)
	if err := lease.CheckPaths([]string{"ABSENT.go"}); err == nil {
		t.Fatal("a different spelling of an absent file was treated as held")
	}
	other := filepath.Join(root, "EXISTING.go")
	if info, statErr := os.Stat(other); statErr == nil {
		held, _ := os.Stat(filepath.Join(root, "existing.go"))
		if os.SameFile(info, held) {
			if err := lease.CheckPaths([]string{"EXISTING.go"}); err != nil {
				t.Fatalf("the same file under another spelling was refused: %v", err)
			}
			return
		}
	}
	if err := lease.CheckPaths([]string{"EXISTING.go"}); err == nil {
		t.Fatal("a distinct file was treated as held")
	}
}
