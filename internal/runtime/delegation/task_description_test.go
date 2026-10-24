package delegation

import (
	"strings"
	"testing"
)

func TestTaskDescriptionSaysASubAgentIsNotANewSession(t *testing.T) {
	desc := (&TaskTool{}).Description()
	for _, want := range []string{"not a new session", "max_subagent_depth"} {
		if !strings.Contains(desc, want) {
			t.Errorf("task description must contain %q so the model can tell a sub-agent from a new session: %s", want, desc)
		}
	}
}

func TestTaskDescriptionSaysOptimisticIsTheDefaultForUndeclaredWrites(t *testing.T) {
	desc := (&TaskTool{}).Description()
	for _, want := range []string{"the default, optimistic mode", "strict mode"} {
		if !strings.Contains(desc, want) {
			t.Errorf("task description must say %q so the model plans for the real default: %s", want, desc)
		}
	}
	if strings.Contains(desc, "Omitting write_paths on a writer claims the whole workspace") {
		t.Errorf("task description still presents the strict behaviour as the default: %s", desc)
	}
}
