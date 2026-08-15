package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrecheckSkipsAlreadyRestoredImage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(target, CaptureBeforeOpts{})
	if err := os.WriteFile(target, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.CaptureAfter(target, CaptureAfterOpts{Seq: 1})
	s.Begin(1, "next", 1)

	if conflicts := s.precheckFiles(0); len(conflicts) != 0 {
		t.Fatalf("after-image should be owned: %+v", conflicts)
	}
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if conflicts := s.precheckFiles(0); len(conflicts) != 0 {
		t.Fatalf("already-restored before-image must not conflict: %+v", conflicts)
	}
}
