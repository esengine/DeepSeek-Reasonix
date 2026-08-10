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
