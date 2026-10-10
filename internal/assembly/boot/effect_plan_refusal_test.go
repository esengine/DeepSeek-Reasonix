package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/planmode"
)

// A call the planning phase refuses reaches the model with the command, the
// class of cause the host established, and the way out. Each call runs in its
// own build because a blocked call makes the rest of its batch skip.
func TestEffectPlanRefusalNamesCommandCauseAndExit(t *testing.T) {
	cases := []struct {
		name   string
		why    planmode.Why
		detail string
		cmd    string
	}{
		{"unknown program", planmode.WhyUnknownProgram, "curl", "curl -s https://example.com"},
		{"variable expansion", planmode.WhyShellConstruct, string(shellparse.StaticRejectExpansion), `ls src/$NAME`},
		{"assignment", planmode.WhyShellConstruct, string(shellparse.StaticRejectAssignment), "A=1; ls"},
		{"redirection", planmode.WhyShellConstruct, string(shellparse.StaticRejectRedirection), "ls > out.txt"},
		{"writing arguments", planmode.WhyWriteArguments, "find", "find . -name x -exec rm {} ;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildPostureRun(t, "", bashCall("c", tc.cmd))
			run.ctrl.SetPlanMode(true)
			_ = run.ctrl.Run(context.Background(), "research")
			saw := run.modelSaw(t, "c")
			for _, want := range []string{"Plan mode is still planning", tc.cmd, "Cause (" + string(tc.why) + ")", tc.detail, "present the plan"} {
				if !strings.Contains(saw, want) {
					t.Fatalf("the model was told %q, missing %q", saw, want)
				}
			}
		})
	}

	t.Run("declared writer", func(t *testing.T) {
		run := buildPostureRun(t, "", writeCall("c", "x.txt"))
		run.ctrl.SetPlanMode(true)
		_ = run.ctrl.Run(context.Background(), "research")
		saw := run.modelSaw(t, "c")
		for _, want := range []string{"write_file", "Cause (" + string(planmode.WhyDeclaredWriter) + ")", "present the plan"} {
			if !strings.Contains(saw, want) {
				t.Fatalf("the model was told %q, missing %q", saw, want)
			}
		}
	})

	t.Run("a proven reader is not refused", func(t *testing.T) {
		run := buildPostureRun(t, "", bashCall("c", "ls"))
		run.ctrl.SetPlanMode(true)
		_ = run.ctrl.Run(context.Background(), "research")
		if saw := run.modelSaw(t, "c"); strings.Contains(saw, "Plan mode is still planning") {
			t.Fatalf("a read-only command was refused while planning: %q", saw)
		}
	})
}
