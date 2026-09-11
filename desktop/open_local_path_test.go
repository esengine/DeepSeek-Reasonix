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
	fileURL := "file://" + filepath.ToSlash(abs)
	if runtime.GOOS == "windows" {
		fileURL = "file:///" + filepath.ToSlash(abs)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"plain absolute", abs, abs},
		{"file URL", fileURL, abs},
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

func TestNormalizeLocalOpenPathAuthorityUNC(t *testing.T) {
	want := filepath.FromSlash("//server/share/docs/report.md")
	got, err := normalizeLocalOpenPath("file://server/share/docs/report.md")
	if err != nil {
		t.Fatalf("authority-form UNC URL rejected: %v", err)
	}
	if got != want {
		t.Fatalf("authority-form UNC URL = %q, want %q", got, want)
	}
}

func TestNormalizeLocalOpenPathRejectsUnsafeWindowsSyntax(t *testing.T) {
	unsafePaths := []string{
		`\\.\PhysicalDrive0`,
		`\\?\C:\Windows\System32`,
		`//./PhysicalDrive0`,
		`//?/C:/Windows/System32`,
		`C:/safe.txt:payload`,
		`C:/docs/NUL.txt`,
		`C:/docs/COM1.log`,
		`//server/share/CON.md`,
		"C:/safe.txt\x00payload",
	}
	for _, path := range unsafePaths {
		if !hasDisallowedWindowsPathSyntax(path) {
			t.Errorf("hasDisallowedWindowsPathSyntax(%q) = false, want true", path)
		}
	}

	unsafeURLs := []string{
		"file://./PhysicalDrive0",
		"file:////./PhysicalDrive0",
		"file:////?/C:/Windows/System32",
		"file:////%3F/C:/Windows/System32",
		"file:///C:/safe.txt:payload",
		"file:///C:/safe.txt%3Apayload",
		"file://user@server/share/report.md",
		"file://server:445/share/report.md",
		"file:///tmp/report.md?download=1",
		"file:///tmp/report.md#section",
	}
	for _, value := range unsafeURLs {
		if got, err := normalizeLocalOpenPath(value); err == nil {
			t.Errorf("normalizeLocalOpenPath(%q) = %q, want error", value, got)
		}
	}
}

func TestNormalizeLocalOpenPathRejectsRelative(t *testing.T) {
	for _, in := range []string{"report.md", "dir/report.md"} {
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
	for _, name := range []string{"report.md", "report.docx", "report.pdf", "diagram.png"} {
		if !openTargetAllowed(filepath.Join("C:", "docs", name), false, 0o644) {
			t.Fatalf("document %q should be openable", name)
		}
	}
	if !openTargetAllowed(filepath.Join("C:", "docs"), true, os.ModeDir|0o755) {
		t.Fatal("directory should be openable")
	}
	for _, name := range []string{
		"evil.bat", "evil.cmd", "evil.exe", "evil.exe.", "evil.exe ", "evil.ps1", "evil.lnk", "evil.url",
		"evil.msi", "run.BAT", "EVIL.Scr", "launcher.desktop",
	} {
		if openTargetAllowed(filepath.Join("C:", "temp", name), false, 0o644) {
			t.Fatalf("executable target %q should be refused", name)
		}
	}
	if openTargetAllowed(filepath.Join("Applications", "Unsafe.app"), true, os.ModeDir|0o755) {
		t.Fatal("macOS application bundle should be refused")
	}
	if openTargetAllowed(filepath.Join("Applications", "Unsafe.app")+string(filepath.Separator), true, os.ModeDir|0o755) {
		t.Fatal("macOS application bundle with trailing separator should be refused")
	}
	if openTargetAllowed(filepath.Join("tmp", "script"), false, 0o755) {
		t.Fatal("executable-mode regular file should be refused")
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
