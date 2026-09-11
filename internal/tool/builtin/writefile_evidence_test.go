package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileDeclaresWholeFileEvidenceOnlyForOverwrites pins the rule that a
// brand-new file needs no prior-content evidence while an overwrite does.
func TestWriteFileDeclaresWholeFileEvidenceOnlyForOverwrites(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := writeFile{workDir: dir}

	info, err := w.DeclareEvidenceTarget(context.Background(), json.RawMessage(`{"path":"a.txt","content":"y"}`))
	if err != nil {
		t.Fatalf("overwrite declaration: %v", err)
	}
	if !info.WholeFile || info.Path != existing {
		t.Fatalf("overwrite target = %+v, want whole-file at %s", info, existing)
	}

	info, err = w.DeclareEvidenceTarget(context.Background(), json.RawMessage(`{"path":"new.txt","content":"y"}`))
	if err != nil {
		t.Fatalf("creation declaration: %v", err)
	}
	if info.WholeFile {
		t.Fatalf("creating a new file must not require prior content: %+v", info)
	}
	if info.Path != filepath.Join(dir, "new.txt") {
		t.Fatalf("creation target path = %q", info.Path)
	}
}

func TestWriteFileDeclareEvidenceTargetRejectsMissingPath(t *testing.T) {
	w := writeFile{workDir: t.TempDir()}
	if _, err := w.DeclareEvidenceTarget(context.Background(), json.RawMessage(`{"content":"y"}`)); err == nil {
		t.Fatal("a write without a path must not declare a target")
	}
}
