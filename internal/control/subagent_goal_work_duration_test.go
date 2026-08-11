package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestSubagentSkillGoalRecordsWorkDuration(t *testing.T) {
	sess := agent.NewSession("")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Executor: exec,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
		Skills: []skill.Skill{{
			Name: "research", Body: "RESEARCH_NOTES", RunAs: skill.RunSubagent, Scope: skill.ScopeGlobal,
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, _ string, _ skill.SubagentRunOptions) (string, error) {
			return "Notes listed.", nil
		},
	})
	defer c.Close()
	c.SetGoalWithResearchMode("list the existing notes", GoalResearchOff)
	c.SubmitInvocationDisplay(
		"list the existing notes",
		"list the existing notes",
		[]InvocationRequest{{Name: "research", Kind: "subagent", Offset: 0}},
	)
	waitForTurnDone(t, events)

	if got := c.GoalRuntime().WorkDurationMs; got <= 0 {
		t.Fatalf("active Goal subagent work duration = %d, want positive", got)
	}
	var childDuration int64
	for _, message := range c.History() {
		if message.Role == provider.RoleAssistant && strings.Contains(message.Content, "Notes listed") {
			childDuration = message.WorkDurationMs
			break
		}
	}
	if childDuration <= 0 {
		t.Fatalf("persisted subagent answer work duration = %d, want positive", childDuration)
	}
}

func TestSubagentSkillGoalRejectsStaleWorkDuration(t *testing.T) {
	sess := agent.NewSession("")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	started := make(chan struct{})
	release := make(chan struct{})
	c := New(Options{
		Executor: exec,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
		Skills: []skill.Skill{{
			Name: "research", Body: "RESEARCH_NOTES", RunAs: skill.RunSubagent, Scope: skill.ScopeGlobal,
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, _ string, _ skill.SubagentRunOptions) (string, error) {
			close(started)
			<-release
			return "stale notes", nil
		},
	})
	defer c.Close()
	c.SetGoalWithResearchMode("original goal", GoalResearchOff)
	c.SubmitInvocationDisplay(
		"inspect the notes",
		"inspect the notes",
		[]InvocationRequest{{Name: "research", Kind: "subagent", Offset: 0}},
	)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("subagent skill did not start")
	}

	c.SetGoalWithResearchMode("replacement goal", GoalResearchOff)
	close(release)
	waitForTurnDone(t, events)

	if got := c.Goal(); got != "replacement goal" {
		t.Fatalf("active Goal = %q, want replacement", got)
	}
	if got := c.GoalRuntime().WorkDurationMs; got != 0 {
		t.Fatalf("stale subagent work duration polluted replacement Goal: %d", got)
	}
}
