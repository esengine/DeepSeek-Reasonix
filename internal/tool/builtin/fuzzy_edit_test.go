package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileFuzzyTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	seed := "func main() {   \n\tfmt.Println(\"hello\")  \n}\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "func main() {\n\tfmt.Println(\"hello\")\n}",
		"new_string": "func main() {\n\tfmt.Println(\"bye\")\n}",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	if !strings.Contains(out, "fuzzy match") {
		t.Fatalf("output should disclose fuzzy matching, got %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "func main() {\n\tfmt.Println(\"bye\")\n}\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileFuzzyReadFileLinePrefixes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	seed := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "1\u2192alpha\n2\u2192beta",
		"new_string": "ALPHA\nBETA",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "ALPHA\nBETA\ngamma\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileFuzzyCRLFPreservesLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "win.txt")
	seed := "one   \r\ntwo   \r\nthree\r\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "one\ntwo",
		"new_string": "ONE\nTWO",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "ONE\r\nTWO\r\nthree\r\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileCRLFNotFoundHintAvoidsMisattribution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "win.txt")
	seed := "one\r\ntwo\r\nthree\r\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "one\nmissing",
		"new_string": "ONE\nMISSING",
	}))
	if err == nil {
		t.Fatal("expected old_string not found")
	}
	msg := err.Error()
	for _, want := range []string{
		"old_string not found",
		"CRLF line endings",
		"already tolerate LF-only old_string",
		"stale, incomplete, or non-unique context",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestEditFileFuzzyAmbiguousDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	seed := "target   \ntarget   \n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "target\n",
		"new_string": "updated\n",
	}))
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("expected not-unique error, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seed {
		t.Fatalf("ambiguous fuzzy edit changed file: %q", got)
	}
}

func TestEditFileFuzzyLeadingIndentDriftDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "indent.go")
	seed := "func f() {\n    if ok {\n        return nil\n    }\n}\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "if ok {\n    return nil\n}",
		"new_string": "if ok {\n    return errors.New(\"nope\")\n}",
	}))
	if err == nil || !strings.Contains(err.Error(), "old_string not found") {
		t.Fatalf("expected not-found error for leading indentation drift, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seed {
		t.Fatalf("leading indentation drift changed file: %q", got)
	}
}

func TestMultiEditFuzzyReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.txt")
	seed := "item   \nitem\t\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := (multiEdit{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path": path,
		"edits": []map[string]any{
			{"old_string": "item\n", "new_string": "thing\n", "replace_all": true},
		},
	}))
	if err != nil {
		t.Fatalf("multi_edit: %v", err)
	}
	if !strings.Contains(out, "2 total replacements") || !strings.Contains(out, "fuzzy match") {
		t.Fatalf("unexpected output: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "thing\nthing\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}
