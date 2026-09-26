package builtin

import (
	"strings"
	"testing"
	"unicode"

	"reasonix/internal/tool"
)

// TestNoBuiltinToolNameHasWhitespace pins the invariant that lets
// permission.UnmatchableRules be exact instead of heuristic: it reports a rule
// whose tool name contains whitespace, which is safe only while no tool is
// named that way. If a future tool takes a name with a space, that check would
// start accusing a rule that actually works, so fix the name, not this test.
func TestNoBuiltinToolNameHasWhitespace(t *testing.T) {
	builtins := tool.Builtins()
	if len(builtins) == 0 {
		t.Fatal("no built-in tools registered; the invariant would pass vacuously")
	}
	for _, tl := range builtins {
		if strings.IndexFunc(tl.Name(), unicode.IsSpace) >= 0 {
			t.Errorf("built-in tool name contains whitespace: %q", tl.Name())
		}
	}
}
