package cli

import (
	"strings"
	"testing"
)

// TestClampWidth guards the inline-overflow fix: scrollback lines wider than the
// viewport get hard-broken (so the renderer's scroll estimate stays exact), while
// lines within width — including space-padded table rows — are left untouched.
func TestClampWidth(t *testing.T) {
	// Within width: byte-for-byte identical (runs of spaces must NOT collapse).
	row := "│ a    │ bb │"
	if got := clampWidth(row, 80); got != row {
		t.Errorf("within-width line altered: %q -> %q", row, got)
	}
	// Over width: every resulting line fits, content is preserved.
	long := strings.Repeat("x", 200)
	out := clampWidth(long, 40)
	for _, line := range strings.Split(out, "\n") {
		if visibleWidth(line) > 40 {
			t.Errorf("clamped line exceeds 40: width=%d", visibleWidth(line))
		}
	}
	if strings.ReplaceAll(out, "\n", "") != long {
		t.Error("clampWidth lost or altered content")
	}
	// width <= 0 is a no-op (pre-sizing).
	if clampWidth(long, 0) != long {
		t.Error("width<=0 should be a no-op")
	}
}
