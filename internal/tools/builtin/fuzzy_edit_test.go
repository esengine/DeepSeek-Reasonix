package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestEditFileFuzzyTrailingWhitespace(t *testing.T) {
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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

// TestEditFileFuzzyBlankLineRun covers the mismatch a PEP 8 source reliably
// produces: the file separates top-level defs with two blank lines and the model
// reproduces the span with one.
func TestEditFileFuzzyBlankLineRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.py")
	seed := "_count = 0\n\n\ndef bump(by=1):\n    global _count\n    _count += by\n    return _count\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "_count = 0\n\ndef bump(by=1):\n    global _count\n    _count += by\n    return _count",
		"new_string": "_count = 0\n\ndef bump(by=1):\n    global _count\n    _count += by * 2\n    return _count",
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
	want := "_count = 0\n\ndef bump(by=1):\n    global _count\n    _count += by * 2\n    return _count\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestEditFileBlankLineRunKeepsUniquenessRule guards the rule that makes the
// relaxation safe: collapsing runs must not turn two candidate spans into one
// silent pick.
func TestEditFileBlankLineRunKeepsUniquenessRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.py")
	seed := "def a():\n    pass\n\n\ndef b():\n    pass\n\n\ndef a():\n    pass\n\n\ndef b():\n    pass\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "def a():\n    pass\n\ndef b():\n    pass",
		"new_string": "def c():\n    pass",
	}))
	if err == nil {
		t.Fatal("expected an ambiguous old_string to be rejected, not silently applied")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seed {
		t.Fatalf("file must be untouched on an ambiguous match, got %q", got)
	}
}

// TestEditFileBlankLineRunDoesNotInventSeparation keeps the relaxation
// one-directional: only a run's length is free, never its presence.
func TestEditFileBlankLineRunDoesNotInventSeparation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tight.py")
	seed := "x = 1\ny = 2\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "x = 1\n\ny = 2",
		"new_string": "x = 9\n\ny = 9",
	}))
	if err == nil {
		t.Fatal("a blank line absent from the file must not match")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seed {
		t.Fatalf("file must be untouched, got %q", got)
	}
}

func TestApplyOldStringEditBlankLineRunCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		old     string
		want    bool
	}{
		{
			name:    "two blank lines reproduced as one",
			content: "def f():\n    return 1\n\n\ndef g():\n    return 2\n",
			old:     "def f():\n    return 1\n\ndef g():\n    return 2",
			want:    true,
		},
		{
			name:    "one blank line reproduced as two",
			content: "a = 1\n\nb = 2\n",
			old:     "a = 1\n\n\nb = 2",
			want:    true,
		},
		{
			name:    "blank run plus trailing whitespace",
			content: "def f():\n    return 1   \n\n\ndef g():\n    return 2\n",
			old:     "def f():\n    return 1\n\ndef g():\n    return 2",
			want:    true,
		},
		{
			name:    "non-blank line still must match",
			content: "def f():\n    return 1\n\n\ndef g():\n    return 2\n",
			old:     "def f():\n    return 99\n\ndef g():\n    return 2",
			want:    false,
		},
		{
			name:    "blank line absent from content",
			content: "a = 1\nb = 2\n",
			old:     "a = 1\n\nb = 2",
			want:    false,
		},
		{
			// Same token count on both sides, so only the blank-vs-non-blank
			// check can reject this; matching would swallow the "b = 2" line.
			name:    "blank line must not consume a non-blank line",
			content: "a = 1\nb = 2\nc = 3\n",
			old:     "a = 1\n\nc = 3",
			want:    false,
		},
		{
			// Nothing but blank lines names no span: it would match any run in
			// any file, which is the opposite of the anchored relaxation.
			name:    "blank-only old_string never matches",
			content: "a\n\n\nb\n",
			old:     "\n\n\n\n\n",
			want:    false,
		},
		{
			name:    "old_string starting in a blank run has no anchor",
			content: "a = 1\n\nb = 2\n",
			old:     "\n\n\nb = 2",
			want:    false,
		},
		{
			// The file has no final newline; matching would write one in.
			name:    "old_string newline must not outrun the file's last line",
			content: "a = 1\n\n\nb = 2",
			old:     "a = 1\n\nb = 2\n",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyOldStringEdit(tt.content, tt.old, "REPLACED", false)
			if (got.applied == 1) != tt.want {
				t.Fatalf("applied=%d matches=%d, want match=%v", got.applied, got.matches, tt.want)
			}
			if tt.want && !got.fuzzy {
				t.Fatal("a blank-run match must be disclosed as fuzzy")
			}
		})
	}
}

// TestMultiEditBlankLineRunReplaceAll covers the replace_all path, where the
// relaxation is allowed to land on every occurrence instead of exactly one.
func TestMultiEditBlankLineRunReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.py")
	seed := "def f():\n    p\n\n\ndef g():\n    p\n\n\ndef f():\n    p\n\n\ndef g():\n    p\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (multiEdit{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path": path,
		"edits": []map[string]any{{
			"old_string":  "def f():\n    p\n\ndef g():\n    p",
			"new_string":  "def fg():\n    p",
			"replace_all": true,
		}},
	}))
	if err != nil {
		t.Fatalf("multi_edit: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "def fg():\n    p\n\n\ndef fg():\n    p\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestEditFileBlankLineRunPreservesCRLF keeps the relaxation compatible with the
// CRLF handling the other fuzzy modes already guarantee.
func TestEditFileBlankLineRunPreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.py")
	seed := "a = 1\r\n\r\n\r\nb = 2\r\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "a = 1\n\nb = 2",
		"new_string": "a = 9\n\nb = 9",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "a = 9\r\n\r\nb = 9\r\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestEditFileBlankLineRunKeepsFinalNewline guards the span end: an old_string
// that omits the file's trailing newline must not consume it.
func TestEditFileBlankLineRunKeepsFinalNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.py")
	seed := "a = 1\n\n\nb = 2\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path":       path,
		"old_string": "a = 1\n\nb = 2",
		"new_string": "a = 9\n\nb = 9",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a = 9\n\nb = 9\n"; string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestMultiEditBlankLineRunReplaceAllRejectsBlankOnly keeps replace_all from
// becoming a file-wide blank-run rewrite: without an anchoring non-blank line,
// a blank-only old_string would land on every run at once.
func TestMultiEditBlankLineRunReplaceAllRejectsBlankOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runs.py")
	seed := "a = 1\n\n\nb = 2\n\n\nc = 3\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (multiEdit{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path": path,
		"edits": []map[string]any{{
			"old_string":  "\n\n\n\n",
			"new_string":  "",
			"replace_all": true,
		}},
	}))
	if err == nil {
		t.Fatal("a blank-only old_string must not match every blank run")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seed {
		t.Fatalf("file must be untouched, got %q", got)
	}
}

func TestEditFileNotFoundNamesBlankLinePresenceDrift(t *testing.T) {
	cases := []struct {
		name, seed, old, span string
	}{
		{
			name: "old_string drops a blank line",
			seed: "tex.uOffset = u;\r\ntex.vOffset = v;\r\n}\r\n\r\n/**\r\n * doc\r\n */\r\n",
			old:  "tex.vOffset = v;\n}\n/**",
			span: "lines 2-5",
		},
		{
			name: "old_string invents a blank line",
			seed: "x = 1\ny = 2\n",
			old:  "x = 1\n\ny = 2",
			span: "lines 1-2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(testenv.TempDir(t), "src.ts")
			if err := os.WriteFile(path, []byte(tc.seed), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
				"path": path, "old_string": tc.old, "new_string": "replaced",
			}))
			if err == nil {
				t.Fatal("a blank line's presence must still not match")
			}
			if msg := err.Error(); !strings.Contains(msg, tc.span) || !strings.Contains(msg, "blank lines") {
				t.Fatalf("error must name the span and the blank-line cause, got %q", msg)
			}
			_, err = (multiEdit{}).Execute(context.Background(), argsJSON(t, map[string]any{
				"path":  path,
				"edits": []map[string]any{{"old_string": tc.old, "new_string": "replaced"}},
			}))
			if err == nil || !strings.Contains(err.Error(), tc.span) {
				t.Fatalf("multi_edit must report the same cause, got %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != tc.seed {
				t.Fatalf("file must be untouched, got %q", got)
			}
		})
	}
}

func TestEditFileNotFoundBlankLineCauseNeedsOneSpan(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "dup.py")
	seed := "a\nb\nz\na\nb\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path": path, "old_string": "a\n\nb", "new_string": "c",
	}))
	if err == nil || strings.Contains(err.Error(), "blank lines") {
		t.Fatalf("two spans match once blank lines are ignored, so no cause may be claimed, got %v", err)
	}
}

func TestEditFileNotFoundBlankLineCauseNeedsDifferentLayout(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "eof.py")
	seed := "a\n\nb"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (editFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"path": path, "old_string": "a\n\nb\n", "new_string": "c\n",
	}))
	if err == nil || strings.Contains(err.Error(), "blank lines") {
		t.Fatalf("the blank lines agree and the miss is the final newline, got %v", err)
	}
}
