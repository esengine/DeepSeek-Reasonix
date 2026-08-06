package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeLocalOpenPath(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "report.md")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"plain absolute", abs, abs},
		{"file URL", "file:///" + filepath.ToSlash(abs), abs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeLocalOpenPath(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("normalizeLocalOpenPath(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeLocalOpenPath(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeLocalOpenPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeLocalOpenPathRejectsRelative(t *testing.T) {
	for _, in := range []string{"report.md", "dir/report.md", "file:///report.md", "file:///dir/report.md"} {
		if _, err := normalizeLocalOpenPath(in); err == nil || !strings.Contains(err.Error(), "not absolute") {
			t.Fatalf("normalizeLocalOpenPath(%q) err = %v, want not-absolute error", in, err)
		}
	}
}

func TestNormalizeLocalOpenPathWindowsUNC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC paths are Windows-only")
	}
	got, err := normalizeLocalOpenPath(`\\server\share\docs\report.md`)
	if err != nil {
		t.Fatalf("UNC path rejected: %v", err)
	}
	if got != `\\server\share\docs\report.md` {
		t.Fatalf("UNC path = %q, want unchanged", got)
	}
}

func TestOpenLocalPathRejectsMissingFile(t *testing.T) {
	a := &App{}
	missing := filepath.Join(t.TempDir(), "does-not-exist.md")
	if err := a.OpenLocalPath(missing); err == nil {
		t.Fatal("OpenLocalPath on a missing path succeeded, want error")
	}
}

func TestOpenLocalPathRejectsRelativeAndEmpty(t *testing.T) {
	a := &App{}
	if err := a.OpenLocalPath(""); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("OpenLocalPath(\"\") err = %v, want os.ErrInvalid", err)
	}
	if err := a.OpenLocalPath("relative.md"); err == nil {
		t.Fatal("OpenLocalPath(relative) succeeded, want error")
	}
}

func TestOpenTargetAllowed(t *testing.T) {
	if !openTargetAllowed(filepath.Join("C:", "docs", "report.md"), false) {
		t.Fatal("markdown document should be openable")
	}
	if !openTargetAllowed(filepath.Join("C:", "docs"), true) {
		t.Fatal("directory should be openable")
	}
	for _, name := range []string{"evil.bat", "evil.cmd", "evil.exe", "evil.ps1", "evil.lnk", "evil.url", "evil.msi", "run.BAT", "EVIL.Scr"} {
		if openTargetAllowed(filepath.Join("C:", "temp", name), false) {
			t.Fatalf("executable target %q should be refused", name)
		}
	}
}

func TestOpenLocalPathRejectsExecutable(t *testing.T) {
	a := &App{}
	dir := t.TempDir()
	script := filepath.Join(dir, "clicked.bat")
	if err := os.WriteFile(script, []byte("@echo off"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenLocalPath(script); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("OpenLocalPath(.bat) err = %v, want executable refusal", err)
	}
}
