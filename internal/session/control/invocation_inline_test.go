package control

import (
	"strings"
	"testing"

	"reasonix/internal/ext/skill"
)

func TestSubmitInvocationDisplayKeepsSharedTextAcrossSeveralInlineSkills(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner: runner,
		Skills: []skill.Skill{
			{Name: "one", Body: "ONE_BODY", RunAs: skill.RunInline, Scope: skill.ScopeGlobal},
			{Name: "two", Body: "TWO_BODY", RunAs: skill.RunInline, Scope: skill.ScopeGlobal},
		},
	})
	c.SubmitInvocationDisplay("/one /two shared task", "shared task", []InvocationRequest{
		{Name: "two", Kind: "skill", Offset: 5},
		{Name: "one", Kind: "skill", Offset: 0},
	})
	waitIdle(t, c)
	if len(runner.inputs) != 1 {
		t.Fatalf("inputs = %q", runner.inputs)
	}
	got := runner.inputs[0]
	one, two := strings.Index(got, "<skill-pin name=\"one\">"), strings.Index(got, "<skill-pin name=\"two\">")
	if one < 0 || two < one || strings.Contains(got, "Arguments:") || !strings.HasSuffix(got, "</skill-pin>\n\nshared task") {
		t.Fatalf("several inline skills should each be pinned with the text appended once:\n%s", got)
	}
}
