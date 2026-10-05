package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/platform/update"
)

// A platform's delta is listed from the mirror with its signature beside it,
// the shared chunk store named; an unsigned index fails the release instead of
// being offered.
func TestManifestListsEachSignedDeltaFromTheMirror(t *testing.T) {
	dir, deltaDir := t.TempDir(), t.TempDir()
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	if err := os.WriteFile(filepath.Join(dir, "ReasonixStudio-windows-amd64-installer.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	platform := filepath.Join(deltaDir, "windows-amd64")
	if err := os.MkdirAll(filepath.Join(deltaDir, "chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(platform, update.DeltaIndexName)
	if err := os.WriteFile(index, []byte("index bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "v0.1.0", "studio-v0.1.0", deltaDir, true); err == nil {
		t.Fatal("an unsigned delta index was published")
	}
	if err := os.WriteFile(index+".minisig", []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "v0.1.0", "studio-v0.1.0", deltaDir, true); err != nil {
		t.Fatalf("run: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	d, ok := m.Deltas["windows-amd64"]
	want := "https://dl.reasonix.io/studio-v0.1.0/delta/windows-amd64/index.json.zst"
	if !ok || d.Index.URL != want || d.Index.Sig != want+".minisig" || d.Index.Size != 11 || d.Chunks != update.StudioChunks {
		t.Fatalf("deltas = %+v", m.Deltas)
	}
	if len(m.Deltas) != 1 {
		t.Fatalf("the chunk store was listed as a platform: %+v", m.Deltas)
	}
}
