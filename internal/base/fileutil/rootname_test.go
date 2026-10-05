package fileutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

// A workspace opened at a drive root was listed as "\": filepath.Base answers
// the separator for a volume, and the sidebar, the tab and the window title all
// showed that.
func TestRootNameOfAVolumeIsTheVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows has volume names")
	}
	for _, root := range []string{`D:\`, `D:`} {
		if got, want := RootName(root), `D:\`; got != want {
			t.Errorf("RootName(%q) = %q, want %q", root, got, want)
		}
	}
	if got := RootName(`\server\share`); got != "share" {
		t.Errorf("RootName of a UNC share = %q, want its last segment", got)
	}
}

// An ordinary directory keeps the name it already had.
func TestRootNameOfADirectoryIsItsBase(t *testing.T) {
	dir := t.TempDir()
	if got, want := RootName(dir), filepath.Base(dir); got != want {
		t.Fatalf("RootName(%q) = %q, want %q", dir, got, want)
	}
	if got := RootName(""); got != "" {
		t.Fatalf("RootName(\"\") = %q, want \"\"", got)
	}
	if got := RootName(string(filepath.Separator)); got == "" {
		t.Fatal("the filesystem root must still be called something")
	}
}
