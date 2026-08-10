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

func TestNormalizeLocalOpenPathRejectsRemoteAuthority(t *testing.T) {
	// A remote host would become a UNC path and trigger an SMB connection
	// (Net-NTLM credential negotiation) on click — must be refused.
	for _, u := range []string{
		"file://attacker.example/share/evil.bat",
		"file://192.168.1.50/share/x.txt",
		// 4+ slashes parse with an empty authority — must still be refused
		// (they normalize to a remote UNC path).
		"file:////attacker.example/share/x.txt",
		"file://///attacker.example/share/x.txt",
		// Loopback authority with a double-slash path: host is dropped, the
		// remaining path still starts with "//" → refused (regression guard
		// for the unconditional check).
		"file://127.0.0.1//attacker.example/share/x.txt",
		"file://localhost//attacker.example/share/x.txt",
		// Backslash spellings: url.Parse does not normalize "\", so the
		// decoded path starts with "/\" — must still be refused after
		// backslash normalization.
		"file:///\\evil.example\\share\\x.txt",
		"file://127.0.0.1/\\evil.example\\share\\x.txt",
		// Case variants of the scheme: FILE:// must be refused too (defense
		// in depth — Windows shell resolves file: scheme case-insensitively).
		"FILE://attacker.example/share/x.txt",
		"File://attacker.example/share/x.txt",
	} {
		if _, err := normalizeLocalOpenPath(u); err == nil {
			t.Errorf("normalizeLocalOpenPath(%q): want rejection (remote authority), got nil", u)
		}
	}
	// Loopback authorities stay allowed; platform-specific resolve assertions
	// live in open_local_path_windows_test.go / _unix_test.go.
	if _, err := normalizeLocalOpenPath("file://localhost/tmp/reasonix-report.md"); err == nil {
		t.Logf("file://localhost accepted (exact resolution is platform-specific)")
	}
	if _, err := normalizeLocalOpenPath("file://127.0.0.1/tmp/reasonix-report.md"); err == nil {
		t.Logf("file://127.0.0.1 accepted (exact resolution is platform-specific)")
	}
}

func TestNormalizeLocalOpenPathRejectsRemoteUNC(t *testing.T) {
	// Plain UNC paths must be refused before any os.Stat/opener call:
	// opening them connects to a remote SMB share (Net-NTLM leak). Loopback
	// UNC is covered in open_local_path_windows_test.go.
	for _, in := range []string{
		`\\server\share\docs\report.md`,
		`//server/share/docs/report.md`,
		`\\attacker\share\x`,
		`//attacker/share/x`,
		`\\\server\share\x`, // triple slash still resolves to a UNC root
	} {
		if _, err := normalizeLocalOpenPath(in); err == nil {
			t.Errorf("normalizeLocalOpenPath(%q): want rejection (remote UNC), got nil", in)
		}
	}
	// The URL spelling is refused for the same reason (SMB credential-leak
	// risk on AI-injected chat content).
	if _, err := normalizeLocalOpenPath("file://server/share/docs/report.md"); err == nil {
		t.Fatal("remote file:// URL must be rejected (SMB leak risk)")
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
	// Remote UNC is refused before any os.Stat/opener call (SMB leak risk).
	for _, in := range []string{
		`\\server\share\docs\report.md`,
		`//server/share/docs/report.md`,
		`\\\server\share\x`,
	} {
		if _, err := normalizeLocalOpenPath(in); err == nil {
			t.Errorf("normalizeLocalOpenPath(%q) = nil, want rejection (remote UNC)", in)
		}
	}
	// Loopback UNC stays allowed — it is this machine.
	got, err := normalizeLocalOpenPath(`\\localhost\share\docs\report.md`)
	if err != nil {
		t.Fatalf("loopback UNC path rejected: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("loopback UNC path = %q, want absolute", got)
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
		"evil.jar", "evil.application", "evil.msc", "evil.cpl", "evil.vbe", "evil.wsh",
		"evil.appref-ms", "evil.ps1xml", "evil.msh", "evil.msh1", "evil.msh2",
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
