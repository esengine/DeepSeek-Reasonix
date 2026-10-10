package proc

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCommandNamesFollowsPATHEXTOnlyOnWindows(t *testing.T) {
	if got := CommandNames("mcp", ".CMD;.EXE", false); !slices.Equal(got, []string{"mcp"}) {
		t.Fatalf("non-Windows names = %v", got)
	}
	if got := CommandNames("mcp", ".CMD;exe;.cmd", true); !slices.Equal(got, []string{"mcp", "mcp.CMD", "mcp.exe"}) {
		t.Fatalf("Windows names = %v", got)
	}
	if got := CommandNames("mcp", "", true); len(got) != 5 {
		t.Fatalf("default PATHEXT names = %v", got)
	}
	if got := CommandNames("mcp.v2", ".CMD", true); !slices.Equal(got, []string{"mcp.v2", "mcp.v2.CMD"}) {
		t.Fatalf("a dotted command is also tried with PATHEXT, as os/exec does: %v", got)
	}
}

func TestLookPathInSkipsRelativeEntriesAndFindsTheSibling(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.cmd"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := "relative" + string(filepath.ListSeparator) + dir
	// The extension is spelled as the file is: Windows lookups ignore case, this
	// test's filesystem may not.
	if got, ok := LookPathIn("mcp", path, ".cmd", true); !ok || got != filepath.Join(dir, "mcp.cmd") {
		t.Fatalf("LookPathIn = %q, %v", got, ok)
	}
	if _, ok := LookPathIn("mcp", path, ".cmd", false); ok {
		t.Fatal("found an extension sibling where the platform does not probe one")
	}
}
