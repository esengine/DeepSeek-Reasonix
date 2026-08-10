package extension

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePriorCompensateRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewFilePriorStore()
	s.Capture("r1", path, []byte("old"), true)
	if err := os.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Compensate("r1"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("got %q", got)
	}
}

func TestFilePriorCompensateRemoveCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("created"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewFilePriorStore()
	s.Capture("c1", path, nil, false)
	if err := s.Compensate("c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected removed, err=%v", err)
	}
}

func TestApplyFileWriteCompensationUpdatesReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(path, []byte("v1"), 0o644)
	id := "file-write:" + path
	DefaultFilePriorStore.Capture(id, path, []byte("v1"), true)
	_ = os.WriteFile(path, []byte("v2"), 0o644)
	if err := ApplyFileWriteCompensation(id); err != nil {
		t.Fatal(err)
	}
	r, ok := DefaultReceiptStore.Get(id)
	if !ok || r.CompensationStatus != "applied" {
		t.Fatalf("receipt = %+v ok=%v", r, ok)
	}
}

func TestFilePriorStoreBoundsRetainedBytes(t *testing.T) {
	s := newFilePriorStore(5, 4)
	dir := t.TempDir()
	if !s.Capture("first", filepath.Join(dir, "first"), []byte("1234"), true) {
		t.Fatal("in-budget prior was rejected")
	}
	if s.Capture("too-large", filepath.Join(dir, "large"), []byte("12345"), true) {
		t.Fatal("oversized prior was retained")
	}
	if s.Capture("over-total", filepath.Join(dir, "total"), []byte("12"), true) {
		t.Fatal("prior exceeding the owner budget was retained")
	}
	if s.retainedBytes != 4 || len(s.byID) != 1 {
		t.Fatalf("retained state = bytes:%d entries:%d, want 4/1", s.retainedBytes, len(s.byID))
	}
	s.Forget("first")
	if s.retainedBytes != 0 || len(s.byID) != 0 {
		t.Fatalf("forget retained state = bytes:%d entries:%d, want 0/0", s.retainedBytes, len(s.byID))
	}
}
