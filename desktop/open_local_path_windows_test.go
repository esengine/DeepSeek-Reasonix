//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestNormalizeLocalOpenPathRejectsDriveLessWindowsFileURL(t *testing.T) {
	for _, value := range []string{"file:///report.md", "file:///dir/report.md"} {
		if got, err := normalizeLocalOpenPath(value); err == nil || !strings.Contains(err.Error(), "not absolute") {
			t.Errorf("normalizeLocalOpenPath(%q) = (%q, %v), want not-absolute error", value, got, err)
		}
	}
}

func TestNormalizeLocalOpenPathLoopbackDriveURL(t *testing.T) {
	// Windows-only: the decoded drive-letter path (C:/x.txt) is only an
	// absolute path under Windows filepath semantics; macOS/Linux reject it as
	// "not absolute" (covered by open_local_path_unix_test.go's neutral case).
	got, err := normalizeLocalOpenPath("file://localhost/C:/x.txt")
	if err != nil {
		t.Fatalf("file://localhost/C:/x.txt: %v", err)
	}
	if !strings.Contains(got, "C:") {
		t.Errorf("file://localhost/C:/x.txt: want drive path, got %q", got)
	}
	if _, err := normalizeLocalOpenPath("file://127.0.0.1/C:/x.txt"); err != nil {
		t.Errorf("file://127.0.0.1/C:/x.txt should be allowed: %v", err)
	}
	if _, err := normalizeLocalOpenPath("file://[::1]/C:/x.txt"); err != nil {
		t.Errorf("file://[::1]/C:/x.txt should be allowed: %v", err)
	}
}
