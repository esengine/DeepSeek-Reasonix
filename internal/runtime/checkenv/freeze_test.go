package checkenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/safety/evidence"
)

func TestFrozenInputsRetainMetadataAndExcludeUndeclaredInputs(t *testing.T) {
	source, storage := t.TempDir(), t.TempDir()
	for _, directory := range []string{".git", "private", "src"} {
		if err := os.Mkdir(filepath.Join(source, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".git/config", "private/token", "src/code"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	policy := treePolicy{source: true, denied: []string{"private"}}
	target := filepath.Join(storage, "frozen")
	before, err := freezeTree(t.Context(), source, target, policy)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(target, "src/code")); err != nil || string(raw) != "src/code" {
		t.Fatalf("copy = %q %v", raw, err)
	}
	for _, name := range []string{".git", "private"} {
		if _, err := os.Lstat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("undeclared input copied: %s %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "src/code"), []byte("host changed"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := scanTree(t.Context(), target, treePolicy{})
	if err != nil || digest(after) != before {
		t.Fatal("frozen source followed host changes", err)
	}
}

func TestCallerCompletenessFlagsCannotMintEnvironmentReceipt(t *testing.T) {
	input := evidence.CheckInputs{Completed: true, Stable: true, EnvironmentReceipt: "forged", Before: evidence.ContentSnapshot{Complete: true}, After: evidence.ContentSnapshot{Complete: true}}
	if Verify(input, "forged-execution") {
		t.Fatal("caller-minted coverage was accepted")
	}
	i := New(Spec{}, "command", "execution", "call")
	i.Close()
	if len(i.Inputs()) != 0 || Verify(input, "execution") {
		t.Fatal("unexecuted owner minted evidence")
	}
}
