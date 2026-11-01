package evidence

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedSegmentCutsOnRuneBoundary(t *testing.T) {
	// 120 bytes into this segment falls inside a three-byte character.
	segment := `python3 -c "print('` + strings.Repeat("测试", 30) + `')"`
	got := boundedSegment(segment)
	if !utf8.ValidString(got) {
		t.Fatalf("boundedSegment split a character: %q", got)
	}
	kept := strings.TrimSuffix(got, "…")
	if kept == got || !strings.HasPrefix(segment, kept) {
		t.Fatalf("boundedSegment = %q, want a prefix of the segment ending in …", got)
	}
	if len(kept) > unprovenSegmentLimit || len(kept) < unprovenSegmentLimit-3 {
		t.Fatalf("boundedSegment kept %d bytes, want just under %d", len(kept), unprovenSegmentLimit)
	}
}

func TestBoundedSegmentKeepsShortSegment(t *testing.T) {
	if got := boundedSegment("  python -c 'print(1)'  "); got != "python -c 'print(1)'" {
		t.Fatalf("boundedSegment = %q", got)
	}
}
