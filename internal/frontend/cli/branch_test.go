package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"reasonix/internal/frontend/termrender"
)

func TestRenderBranchTreeStylesVisualWeight(t *testing.T) {
	defer termrender.SetColorProfile(termrender.SetColorProfile(colorprofile.ANSI256))

	got := renderBranchTree("branches:\n├─ 0601-030143.318  你是谁  3 turns\n│  └─ 0601-033937.165  JSON response: success  1 turn\n└─ 0601-035153.346  JSON array  1 turn  current")
	for _, want := range []string{
		termrender.Accent("branches:"),
		termrender.Dim("├─ "),
		termrender.Dim("0601-030143.318"),
		termrender.Dim("│  └─ "),
		termrender.Dim("0601-033937.165"),
		termrender.Dim("3 turns"),
		termrender.Accent("current"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled tree missing %q:\n%q", want, got)
		}
	}
	if strings.Contains(got, "*") {
		t.Fatalf("styled tree should not use a duplicate current marker:\n%q", got)
	}
}
