package cli

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

type rewindConfirmationController struct {
	control.SessionAPI
	plan     checkpoint.RewindPlan
	commits  []string
	result   checkpoint.RewindResult
	switches []string
}

func (c *rewindConfirmationController) PrepareRewind(_ int, _ control.RewindScope) (checkpoint.RewindPlan, error) {
	return c.plan, nil
}

func (c *rewindConfirmationController) CommitRewind(planID string) (checkpoint.RewindResult, error) {
	c.commits = append(c.commits, planID)
	if c.result.ConversationForked || c.result.Branch != "" {
		return c.result, nil
	}
	return checkpoint.RewindResult{OK: true}, nil
}

func (c *rewindConfirmationController) SwitchBranch(ref string) (agent.BranchInfo, error) {
	c.switches = append(c.switches, ref)
	return agent.BranchInfo{Path: ref}, nil
}

func (c *rewindConfirmationController) SetPlanMode(bool)            {}
func (c *rewindConfirmationController) History() []provider.Message { return nil }

func (c *rewindConfirmationController) SummarizeFrom(context.Context, int) error { return nil }
func (c *rewindConfirmationController) SummarizeUpTo(context.Context, int) error { return nil }

func TestOneLine(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	if got := oneLine("", 10); got != "(empty)" {
		t.Fatalf("empty -> %q", got)
	}
	if got := oneLine("a\nb\nc", 10); strings.Contains(got, "\n") {
		t.Fatalf("oneLine kept a newline: %q", got)
	}
	if got := oneLine("this is a fairly long prompt", 8); len([]rune(got)) > 8 {
		t.Fatalf("oneLine did not truncate to width: %q", got)
	}
}

func TestRenderRewindSmoke(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	metas := []checkpoint.Meta{
		{Turn: 0, Prompt: "add the parser", Paths: []string{"a.go"}},
		{Turn: 1, Prompt: "fix the bug", Paths: []string{"b.go", "c.go"}},
	}
	// Stage 0: turn list.
	m := chatTUI{width: 80, rewind: &rewindPicker{metas: metas, sel: 1}}
	out := m.renderRewind()
	if out == "" || !strings.Contains(out, "Rewind") || !strings.Contains(out, "fix the bug") {
		t.Fatalf("stage-0 render missing content:\n%s", out)
	}
	// Stage 1: scope menu.
	m.rewind.stage = 1
	out = m.renderRewind()
	for _, want := range []string{"Restore to turn 2", "Code + conversation", "Conversation only", "Code only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stage-1 render missing %q:\n%s", want, out)
		}
	}
	// Closed picker renders nothing.
	m.rewind = nil
	if out := m.renderRewind(); out != "" {
		t.Fatalf("closed picker rendered %q", out)
	}
}

func TestPartialCoverageRequiresExplicitConfirmation(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	partial := checkpoint.RewindPlan{
		PlanID:   "plan-partial",
		Scope:    checkpoint.RewindBoth,
		CanFiles: true, CanConversation: true,
		Coverage:     checkpoint.CoveragePartial,
		CoverageGaps: []checkpoint.CoverageGap{{Reason: checkpoint.GapBashSideEffect}},
	}
	if !rewindPlanCanApply(partial) || !control.RewindPlanRequiresConfirmation(partial) {
		t.Fatalf("partial plan should be applicable only after confirmation: %+v", partial)
	}
	complete := partial
	complete.Coverage = checkpoint.CoverageComplete
	complete.CoverageGaps = nil
	if control.RewindPlanRequiresConfirmation(complete) {
		t.Fatal("complete coverage should not require an extra confirmation")
	}
	conversationOnly := partial
	conversationOnly.Scope = checkpoint.RewindConversation
	if control.RewindPlanRequiresConfirmation(conversationOnly) {
		t.Fatal("conversation-only rewind should not warn about file coverage")
	}

	m := chatTUI{width: 80, rewind: &rewindPicker{
		metas:       []checkpoint.Meta{{Turn: 0, Prompt: "change files"}},
		stage:       2,
		pendingPlan: partial,
	}}
	out := m.renderRewind()
	if !strings.Contains(out, "Partial file coverage") || !strings.Contains(out, "1 coverage gap") || !strings.Contains(out, "Enter/y confirm") {
		t.Fatalf("confirmation render missing coverage warning:\n%s", out)
	}
	next, _ := m.handleRewindKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(chatTUI)
	if m.rewind == nil || m.rewind.stage != 1 || m.rewind.pendingPlan.PlanID != "" {
		t.Fatalf("Esc should return to scope selection and discard the prepared confirmation: %+v", m.rewind)
	}
}

func TestApplyRewindDoesNotCommitPartialCoverageBeforeConfirmation(t *testing.T) {
	plan := checkpoint.RewindPlan{
		PlanID: "plan-partial", Scope: checkpoint.RewindCode,
		CanFiles: true, Coverage: checkpoint.CoveragePartial,
		CoverageGaps: []checkpoint.CoverageGap{{Reason: checkpoint.GapBashSideEffect}},
	}
	ctrl := &rewindConfirmationController{plan: plan}
	m := chatTUI{
		ctrl:  ctrl,
		width: 80,
		rewind: &rewindPicker{
			metas: []checkpoint.Meta{{Turn: 0, Prompt: "change files"}},
			stage: 1,
			scope: 2,
		},
	}

	next, _ := m.handleRewindKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(chatTUI)
	if m.rewind == nil || m.rewind.stage != 2 || len(ctrl.commits) != 0 {
		t.Fatalf("partial rewind should pause for confirmation: picker=%+v commits=%v", m.rewind, ctrl.commits)
	}
	next, _ = m.handleRewindKey(tea.KeyPressMsg{Code: 'y'})
	m = next.(chatTUI)
	if m.rewind != nil || len(ctrl.commits) != 1 || ctrl.commits[0] != plan.PlanID {
		t.Fatalf("confirmed rewind should commit once: picker=%+v commits=%v", m.rewind, ctrl.commits)
	}
}

func TestConversationRewindActivatesReturnedFork(t *testing.T) {
	plan := checkpoint.RewindPlan{
		PlanID: "plan-conversation", Scope: checkpoint.RewindConversation,
		CanConversation: true,
	}
	ctrl := &rewindConfirmationController{
		plan: plan,
		result: checkpoint.RewindResult{
			OK: true, ConversationForked: true, ConversationOK: true,
			Branch: "/sessions/fork.jsonl",
		},
	}
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.rewind = &rewindPicker{
		metas:       []checkpoint.Meta{{Turn: 0}},
		pendingPlan: plan,
	}

	next, _ := m.commitPreparedRewind()
	m = next.(chatTUI)
	if len(ctrl.switches) != 1 || ctrl.switches[0] != ctrl.result.Branch {
		t.Fatalf("rewind fork switches = %v, want %q", ctrl.switches, ctrl.result.Branch)
	}
	if !m.sessionSwitch {
		t.Fatal("conversation rewind did not replay the forked session")
	}
}
