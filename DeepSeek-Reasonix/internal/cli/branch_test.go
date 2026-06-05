package cli

import (
	"strings"
	"testing"
)

func TestRenderBranchTreeStylesVisualWeight(t *testing.T) {
	oldColor := colorEnabled
	colorEnabled = true
	defer func() { colorEnabled = oldColor }()

	got := renderBranchTree("branches:\n├─ 0601-030143.318  你是谁  3 turns\n│  └─ 0601-033937.165  JSON response: success  1 turn\n└─ 0601-035153.346  JSON array  1 turn  current")
	for _, want := range []string{
		accent("branches:"),
		dim("├─ "),
		dim("0601-030143.318"),
		dim("│  └─ "),
		dim("0601-033937.165"),
		dim("3 turns"),
		accent("current"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled tree missing %q:\n%q", want, got)
		}
	}
	if strings.Contains(got, "*") {
		t.Fatalf("styled tree should not use a duplicate current marker:\n%q", got)
	}
}
