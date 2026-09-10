package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/tool"
)

func TestWritePreflightRejectsRacedCreationAndDisappearance(t *testing.T) {
	for _, wasPresent := range []bool{false, true} {
		dir := t.TempDir()
		p := filepath.Join(dir, "file")
		if wasPresent {
			if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		w := writeFile{workDir: dir}
		args := json.RawMessage(`{"path":"file","content":"replacement"}`)
		target, err := w.DeclareEvidenceTarget(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if wasPresent {
			err = os.Remove(p)
		} else {
			err = os.WriteFile(p, []byte("raced"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.Execute(tool.WithExpectedWriteSource(context.Background(), target), args)
		var opErr *tool.OperationError
		if !errors.As(err, &opErr) || opErr.Diagnostic.Code != tool.WriteEvidenceStale {
			t.Fatalf("presence race accepted: %v", err)
		}
	}
}

func TestMovePreflightPreservesBinaryAndRejectsChangedSource(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "dest")
	if err := os.WriteFile(src, []byte{0, 1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	m := moveFile{workDir: dir}
	args := json.RawMessage(`{"source_path":"source","destination_path":"dest"}`)
	info, err := m.DeclareEvidenceTarget(context.Background(), args)
	if err != nil || !info.PreservesContent {
		t.Fatalf("declaration: %+v %v", info, err)
	}
	if err := os.WriteFile(src, []byte{3, 2, 1, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Execute(tool.WithExpectedWriteSource(context.Background(), info), args); !errors.Is(err, ErrFileChanged) {
		t.Fatalf("stale move accepted: %v", err)
	}
	if _, err = os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("stale move created destination")
	}
	info, err = m.DeclareEvidenceTarget(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Execute(tool.WithExpectedWriteSource(context.Background(), info), args); err != nil {
		t.Fatal(err)
	}
}
