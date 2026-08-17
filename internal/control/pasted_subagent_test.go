package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestStructuredResearchReceivesExpandedPastedText(t *testing.T) {
	const label = "[Pasted text #1 · 536 lines]"
	const bodyMarker = "line-536-body-marker"
	expanded := label + "\n\n--- Begin " + label + " ---\nfirst line\n" + bodyMarker + "\n--- End " + label + " ---"
	display := "/research " + label

	sess := agent.NewSession("parent system")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 12)
	var gotTask string
	var gotDisplay string
	c := New(Options{
		Executor: exec,
		Sink:     event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{{
			Name: "research", Body: "research system", RunAs: skill.RunSubagent,
			Invocation: "manual", Scope: skill.ScopeBuiltin,
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, task string, _ skill.SubagentRunOptions) (string, error) {
			gotTask = task
			return "research answer", nil
		},
	})
	defer c.Close()
	c.SetDisplayRecorder(func(_, shown string) { gotDisplay = shown })

	c.SubmitInvocationDisplay(display, expanded, []InvocationRequest{{Name: "research", Kind: "subagent", Offset: 0}})
	waitForTurnEvents(t, events)
	waitIdle(t, c)

	if !strings.Contains(gotTask, bodyMarker) || !strings.Contains(gotTask, "--- Begin "+label+" ---") {
		t.Fatalf("research task lost expanded pasted text: %q", gotTask)
	}
	if gotDisplay != display {
		t.Fatalf("display = %q, want folded invocation %q", gotDisplay, display)
	}
	msgs := c.History()
	if len(msgs) != 3 || msgs[1].Role != provider.RoleUser || !strings.Contains(msgs[1].Content, bodyMarker) {
		t.Fatalf("parent history lost expanded pasted text: %#v", msgs)
	}
}
