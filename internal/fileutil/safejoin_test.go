package fileutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeJoinPreservesSameDrive(t *testing.T) {
	got := SafeJoin("/base", "sub", "file.txt")
	want := filepath.Join("/base", "sub", "file.txt")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoinAbsoluteOverridesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific cross-drive test")
	}
	// Go's filepath.Join produces the wrong result on Windows for cross-drive paths.
	// SafeJoin must fix it.
	got := SafeJoin(`E:\proj`, `C:\Users\test\file.txt`)
	want := `C:\Users\test\file.txt`
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoinRightmostAbsoluteWins(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific cross-drive test")
	}
	// Rightmost absolute path should anchor the result.
	got := SafeJoin(`E:\base`, `C:\first`, `D:\second\file.txt`)
	want := `D:\second\file.txt`
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoinEmptyReturnsEmpty(t *testing.T) {
	got := SafeJoin()
	if got != "" {
		t.Errorf("SafeJoin() = %q, want empty", got)
	}
}

func TestSafeJoinSingleElement(t *testing.T) {
	got := SafeJoin("/single")
	if got != "/single" && got != "\\single" && got != `C:\single` {
		t.Errorf("SafeJoin single element = %q, want the element itself", got)
	}
}

func TestSafeJoinRelativePaths(t *testing.T) {
	got := SafeJoin("a", "b", "c")
	want := filepath.Join("a", "b", "c")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}
