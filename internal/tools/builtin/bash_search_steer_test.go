package builtin

import (
	"slices"
	"strings"
	"testing"
)

// grep and glob are deferred tools the model reaches as tool:grep through
// use_capability; steering to a name the provider schema hides would read as
// unreliable and train the shell fallback it exists to prevent.
func TestBashDescriptionSteersSearchToTheDeferredBuiltIns(t *testing.T) {
	desc := (bash{}).Description()
	for _, want := range []string{"tool:grep", "tool:glob", "use_capability", "not grep, rg or find in the shell", "git log | grep"} {
		if !strings.Contains(desc, want) {
			t.Errorf("bash description lacks %q\n%s", want, desc)
		}
	}
}

func TestBashSteerToolsNameOnlyWhatTheModelCanCall(t *testing.T) {
	got := BashSteerTools()
	if !slices.Contains(got, "use_capability") {
		t.Errorf("BashSteerTools() = %v, missing use_capability", got)
	}
	for _, hidden := range []string{"grep", "glob"} {
		if slices.Contains(got, hidden) {
			t.Errorf("BashSteerTools() = %v names %q, which the provider schema hides", got, hidden)
		}
	}
}
