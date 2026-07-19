package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// TestIsEditFileAnchorError verifies that isEditFileAnchorError correctly
// identifies retryable edit_file / multi_edit errors with Default level.
func TestIsEditFileAnchorError(t *testing.T) {
	level := provider.AnchorEditDefault
	tests := []struct {
		name string
		tool string
		err  string
		want bool
	}{
		{"edit_file not found", "edit_file", `old_string not found in "foo.go"`, true},
		{"edit_file not unique", "edit_file", `old_string is not unique in "foo.go"`, true},
		{"multi_edit not found", "multi_edit", "edit 1: old_string not found", true},
		{"multi_edit not unique", "multi_edit", "edit 2: old_string is not unique", true},
		{"edit_file write error", "edit_file", "write foo.go: permission denied", false},
		{"edit_file read error", "edit_file", "read foo.go: no such file", false},
		{"bash error", "bash", "old_string not found", false},
		{"write_file error", "write_file", "old_string not found", false},
		{"multi_edit not found variant", "multi_edit", `edit 1: old_string not found in "main.go"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &editFileErrorStub{msg: tt.err}
			if got := isEditFileAnchorError(tt.tool, err, level); got != tt.want {
				t.Errorf("isEditFileAnchorError(%q, %q, Default) = %v, want %v",
					tt.tool, tt.err, got, tt.want)
			}
		})
	}
}

// TestIsEditFileAnchorErrorAggressive verifies that with Aggressive level,
// every edit_file/multi_edit error triggers annotation, including write
// and read errors that would not trigger at Default level.
func TestIsEditFileAnchorErrorAggressive(t *testing.T) {
	level := provider.AnchorEditAggressive
	tests := []struct {
		name string
		tool string
		err  string
		want bool
	}{
		{"edit_file not found", "edit_file", `old_string not found`, true},
		{"edit_file write error", "edit_file", "write foo.go: permission denied", true},
		{"edit_file read error", "edit_file", "read foo.go: no such file", true},
		{"multi_edit any error", "multi_edit", "edit 3: some unexpected error", true},
		{"bash error ignores aggressive", "bash", "old_string not found", false},
		{"write_file error ignores aggressive", "write_file", "permission denied", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &editFileErrorStub{msg: tt.err}
			if got := isEditFileAnchorError(tt.tool, err, level); got != tt.want {
				t.Errorf("isEditFileAnchorError(%q, %q, Aggressive) = %v, want %v",
					tt.tool, tt.err, got, tt.want)
			}
		})
	}
}

// editFileErrorStub is a minimal error that lets us test isEditFileAnchorError
// without constructing the actual fmt.Errorf messages from editfile.go.
type editFileErrorStub struct{ msg string }

func (e *editFileErrorStub) Error() string { return e.msg }

func TestReadFileContext(t *testing.T) {
	// Create a temp file with known content.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := `package main

func foo() {
	fmt.Println("hello")
}

func bar() {
	fmt.Println("world")
}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Test: readFileContext returns file contents for a valid absolute path.
	pathOK := filepath.ToSlash(path)
	args := `{"path":"` + pathOK + `"}`
	ctx := readFileContext([]byte(args))
	if ctx == "" {
		t.Fatal("readFileContext returned empty string for valid absolute path")
	}

	// Test: relative path is resolved correctly (the real bug from #6337).
	// Change to the temp dir so a relative path points there.
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(oldDir) })
	args = `{"path":"test.go"}`
	ctx = readFileContext([]byte(args))
	if ctx == "" {
		t.Fatal("readFileContext returned empty string for valid relative path")
	}

	// Test: missing path returns empty.
	ctx = readFileContext([]byte(`{}`))
	if ctx != "" {
		t.Errorf("expected empty for missing path, got %q", ctx)
	}

	// Test: invalid JSON returns empty.
	ctx = readFileContext([]byte(`not-json`))
	if ctx != "" {
		t.Errorf("expected empty for invalid JSON, got %q", ctx)
	}

	// Test: non-existent file returns empty (doesn't crash).
	args = `{"path":"/nonexistent/file.go"}`
	ctx = readFileContext([]byte(args))
	if ctx != "" {
		t.Errorf("expected empty for non-existent file, got %q", ctx)
	}
}
