package lsp

import (
	"bufio"
	"strings"
	"testing"
)

// TestReadFrameRejectsOversizeContentLength proves a corrupt or hostile
// Content-Length is rejected before the body is allocated — a gigabyte length
// must not trigger a gigabyte make([]byte). The reader holds no body, so the only
// way this returns without hanging or OOMing is the cap check.
func TestReadFrameRejectsOversizeContentLength(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("Content-Length: 9999999999999\r\n\r\n"))
	if _, err := readFrame(r); err == nil {
		t.Fatal("oversize Content-Length should be rejected")
	} else if !strings.Contains(err.Error(), "frame cap") {
		t.Fatalf("want a frame-cap error, got %v", err)
	}
}
