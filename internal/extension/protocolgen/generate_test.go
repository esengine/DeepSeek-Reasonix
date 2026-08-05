package protocolgen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedArtifactsAreDeterministicAndCommitted(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 || len(second) != len(first) {
		t.Fatalf("generated %d/%d artifacts, want 4", len(first), len(second))
	}

	temporaryRoot := t.TempDir()
	if err := Write(temporaryRoot, first); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for i, artifact := range first {
		if artifact.Path != second[i].Path || !bytes.Equal(artifact.Data, second[i].Data) {
			t.Fatalf("artifact %s is not deterministic", artifact.Path)
		}
		generated, err := os.ReadFile(filepath.Join(temporaryRoot, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Fatalf("committed artifact drift: %s (run go run ./cmd/extension-protocol-gen -root .)", artifact.Path)
		}
	}
}

func TestCheckRejectsAnyArtifactDrift(t *testing.T) {
	artifacts, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := Write(root, artifacts); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, artifacts); err != nil {
		t.Fatalf("freshly generated artifacts failed check: %v", err)
	}
	drifted := filepath.Join(root, filepath.FromSlash(artifacts[1].Path))
	if err := os.WriteFile(drifted, append(append([]byte(nil), artifacts[1].Data...), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, artifacts); err == nil {
		t.Fatal("Check accepted a drifted generated artifact")
	}
}
