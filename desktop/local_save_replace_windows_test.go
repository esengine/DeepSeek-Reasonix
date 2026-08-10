//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceLocalSaveDestinationFailurePreservesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.md")
	original := []byte("existing destination")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	missingTemp := filepath.Join(dir, "missing.reasonix-copy")
	if err := replaceLocalSaveDestination(missingTemp, target); err == nil {
		t.Fatal("replaceLocalSaveDestination with missing source succeeded, want error")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("existing destination was removed after failed replacement: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("existing destination changed after failed replacement: got %q, want %q", got, original)
	}
}
