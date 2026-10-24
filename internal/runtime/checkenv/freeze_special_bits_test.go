//go:build !windows

package checkenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFreezingAcceptsSpecialPermissionBitsItCannotReproduce(t *testing.T) {
	source, storage := t.TempDir(), t.TempDir()
	shared := filepath.Join(source, "tmp")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(source, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tool, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	frozen, err := freezeTree(t.Context(), source, filepath.Join(storage, "copy"), treePolicy{})
	if err != nil {
		t.Fatalf("special bits made every freeze differ from its input: %v", err)
	}
	original, err := scanTree(t.Context(), source, treePolicy{})
	if err != nil || digest(original) != frozen {
		t.Fatalf("an unchanged source reads as drift after freezing: %v", err)
	}
}
