package tool

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestMissingRootNamesTheFirstGoneDirectoryByItsDisplayPath(t *testing.T) {
	dir := testenv.TempDir(t)
	gone := filepath.Join(dir, "gone")
	roots := []Root{
		{Kind: RootWorkspace, Path: dir, Display: dir},
		{Kind: RootExternal, Path: gone, Display: "__reasonix_external_folder/abc/meta"},
	}
	err := MissingRoot(roots, os.Stat)
	if !errors.Is(err, ErrRootMissing) {
		t.Fatalf("a root that is gone must match ErrRootMissing, got %v", err)
	}
	var missing *RootMissingError
	if !errors.As(err, &missing) || missing.Which() != RootExternal {
		t.Fatalf("want the external root, got %#v", err)
	}
	if strings.Contains(err.Error(), gone) {
		t.Fatalf("the real path of an external folder must not reach the model: %s", err)
	}
	if !strings.Contains(err.Error(), "__reasonix_external_folder/abc/meta") || !strings.Contains(err.Error(), "not a permission or argument problem") {
		t.Fatalf("the text must name the display path and say what kind of failure this is: %s", err)
	}
	if got := refusalOf(err); got != CodeRootMissing {
		t.Fatalf("the refusal identity = %q, want %q", got, CodeRootMissing)
	}
}

func refusalOf(err error) string {
	var id RefusalIdentity
	if errors.As(err, &id) {
		return id.RefusalCode()
	}
	return ""
}

func TestMissingRootLeavesOtherStatFailuresToTheTool(t *testing.T) {
	roots := []Root{{Kind: RootWorkspace, Path: "/x", Display: "/x"}}
	denied := func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
	if err := MissingRoot(roots, denied); err != nil {
		t.Fatalf("a denied stat is the tool's to report, got %v", err)
	}
}

func TestMissingRootTreatsAFileWhereADirectoryShouldBeAsMissing(t *testing.T) {
	file := filepath.Join(testenv.TempDir(t), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MissingRoot([]Root{{Kind: RootWorkspace, Path: file, Display: file}}, os.Stat); !errors.Is(err, ErrRootMissing) {
		t.Fatalf("a file standing where the project folder was is not a project folder, got %v", err)
	}
}

func TestMissingRootIgnoresRootsWithNoPath(t *testing.T) {
	if err := MissingRoot([]Root{{Kind: RootWorkspace}}, os.Stat); err != nil {
		t.Fatalf("a call with no directory has nothing to be missing, got %v", err)
	}
}
