package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileRejectsChangeBetweenReadAndPublish(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(f, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := readEditSource(context.Background(), nil, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := src.write(context.Background(), nil, f, "new"); !errors.Is(err, ErrFileChanged) {
		t.Fatalf("write after external edit: %v, want ErrFileChanged", err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "external" {
		t.Fatalf("destination overwritten: %q", got)
	}
}

func TestWriteFileRejectsCreateWhenFileAppears(t *testing.T) {
	f := filepath.Join(t.TempDir(), "new.txt")
	src, err := readEditSource(context.Background(), nil, f)
	if !os.IsNotExist(err) {
		t.Fatalf("missing file err = %v, want NotExist", err)
	}
	if err := os.WriteFile(f, []byte("raced"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := src.write(context.Background(), nil, f, "created"); !errors.Is(err, ErrFileChanged) {
		t.Fatalf("create after race: %v, want ErrFileChanged", err)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "raced" {
		t.Fatalf("raced file overwritten: %q", got)
	}
}

type mutatingOverlay struct {
	content string
	writes  int
}

func (m *mutatingOverlay) ReadTextFile(ctx context.Context, path string) (string, bool) {
	return m.content, true
}

func (m *mutatingOverlay) WriteTextFile(ctx context.Context, path, content string) (bool, error) {
	m.writes++
	m.content = content
	return true, nil
}

func TestEditSourceRejectsOverlayChange(t *testing.T) {
	f := filepath.Join(t.TempDir(), "buf.txt")
	if err := os.WriteFile(f, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	ov := &mutatingOverlay{content: "buffer"}
	src, err := readEditSource(context.Background(), ov, f)
	if err != nil {
		t.Fatal(err)
	}
	ov.content = "someone else typed"
	if err := src.write(context.Background(), ov, f, "tool write"); !errors.Is(err, ErrFileChanged) {
		t.Fatalf("overlay write: %v, want ErrFileChanged", err)
	}
	if ov.writes != 0 {
		t.Fatalf("overlay write count = %d, want 0", ov.writes)
	}
}
