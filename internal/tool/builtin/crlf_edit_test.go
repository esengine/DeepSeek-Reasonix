package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func encGBK(t *testing.T, s string) []byte {
	t.Helper()
	out, err := simplifiedchinese.GB18030.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(out)
}

func decGBK(t *testing.T, b []byte) string {
	t.Helper()
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func runEdit(t *testing.T, dir, name, oldS, newS string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"path": name, "old_string": oldS, "new_string": newS})
	if _, err := (editFile{workDir: dir}).Execute(context.Background(), b); err != nil {
		t.Fatalf("edit_file: %v", err)
	}
}

func TestEditFileCRLFGBKPreservesEncodingAndEndings(t *testing.T) {
	dir := t.TempDir()
	src := "第一行 hello\r\n第二行 world\r\n第三行 done\r\n"
	if err := os.WriteFile(filepath.Join(dir, "f.cs"), encGBK(t, src), 0o644); err != nil {
		t.Fatal(err)
	}

	// old/new arrive LF-only, as the model copies them out of read_file output.
	runEdit(t, dir, "f.cs", "第一行 hello\n第二行 world", "第一行 HELLO\n第二行 WORLD")

	raw, err := os.ReadFile(filepath.Join(dir, "f.cs"))
	if err != nil {
		t.Fatal(err)
	}
	got := decGBK(t, raw)
	want := "第一行 HELLO\r\n第二行 WORLD\r\n第三行 done\r\n"
	if got != want {
		t.Fatalf("content/endings not preserved:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("a bare \\n leaked into a CRLF file: %q", got)
	}
}

func TestEditFileLFStaysLF(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runEdit(t, dir, "f.txt", "alpha\nbeta", "ALPHA\nBETA")
	raw, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if strings.Contains(string(raw), "\r") {
		t.Fatalf("CR leaked into an LF file: %q", string(raw))
	}
	if string(raw) != "ALPHA\nBETA\ngamma\n" {
		t.Fatalf("unexpected result: %q", string(raw))
	}
}

func TestMultiEditCRLF(t *testing.T) {
	dir := t.TempDir()
	src := "one\r\ntwo\r\nthree\r\nfour\r\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"old_string": "one\ntwo", "new_string": "ONE\nTWO"},
			{"old_string": "three", "new_string": "THREE"},
		},
	})
	if _, err := (multiEdit{workDir: dir}).Execute(context.Background(), b); err != nil {
		t.Fatalf("multi_edit: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(raw) != "ONE\r\nTWO\r\nTHREE\r\nfour\r\n" {
		t.Fatalf("multi_edit mangled endings: %q", string(raw))
	}
}
