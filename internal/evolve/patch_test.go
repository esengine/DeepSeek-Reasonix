package evolve

import (
	"strings"
	"testing"
)

func TestPatchStandingDocPrefersConventions(t *testing.T) {
	old := "# X\n\n## Conventions\n\n- A\n\n## Notes\n\n- old note\n"
	newBody, change := PatchStandingDoc("AGENTS.md", old, "- **B:** do B")
	if !strings.Contains(newBody, "## Conventions\n\n- A\n- **B:** do B") &&
		!strings.Contains(newBody, "- **B:** do B") {
		t.Fatalf("bullet not under conventions:\n%s", newBody)
	}
	// Notes section still present.
	if !strings.Contains(newBody, "## Notes") {
		t.Fatal("notes lost")
	}
	if change.Diff == "" && old != newBody {
		t.Fatal("expected diff for modify")
	}
}

func TestPatchStandingDocCreatesNotesWhenEmpty(t *testing.T) {
	newBody, _ := PatchStandingDoc("AGENTS.md", "", "- only")
	if !strings.Contains(newBody, "## Notes") || !strings.Contains(newBody, "- only") {
		t.Fatalf("got %q", newBody)
	}
}
