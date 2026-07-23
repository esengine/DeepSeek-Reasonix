package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// runHeadlessWriteOnce drives one write_file tool call through a headless gate in
// the given mode, with the given explicit ask rules, and reports how many
// approval prompts were emitted (must always be 0 headless) and which paths were
// actually written. It fails the test if the turn blocks (a wrongful prompt would
// hang forever under the zero approval timeout).
func runHeadlessWriteOnce(t *testing.T, mode string, askRules []string) (prompts int, written []string) {
	t.Helper()
	writer := &recordingWriter{}
	reg := tool.NewRegistry()
	reg.Add(writer)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "write_file", `{"path":"a.txt"}`),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, askRules, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
			}
		}),
		// ApprovalTimeout intentionally zero: a wrongful prompt would block forever.
	})
	c.ApplyHeadlessApprovalMode(mode)

	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), "edit", "edit") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurnWithRaw: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("headless %s must not block on a write", mode)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return prompts, append([]string(nil), writer.paths...)
}

// TestApplyHeadlessApprovalModeAutoDeniesExplicitAskRule pins the corrected auto
// contract: a command the config explicitly marked "ask" must NOT run silently
// under headless auto (there is no one to approve it), yet must not prompt or
// hang either. auto preserves explicit ask rules by failing closed.
func TestApplyHeadlessApprovalModeAutoDeniesExplicitAskRule(t *testing.T) {
	prompts, written := runHeadlessWriteOnce(t, ToolApprovalAuto, []string{"write_file"})
	if prompts != 0 {
		t.Fatalf("approval prompts = %d, want 0 (headless run has no UI to answer)", prompts)
	}
	if len(written) != 0 {
		t.Fatalf("executed writes = %v, want none (auto must not silently run an explicit ask rule)", written)
	}
}

// TestApplyHeadlessApprovalModeAutoAllowsWriterFallback confirms auto still
// auto-approves the ordinary writer fallback (no explicit rule): that is the
// permissiveness auto is meant to add over the default headless gate.
func TestApplyHeadlessApprovalModeAutoAllowsWriterFallback(t *testing.T) {
	prompts, written := runHeadlessWriteOnce(t, ToolApprovalAuto, nil)
	if prompts != 0 {
		t.Fatalf("approval prompts = %d, want 0", prompts)
	}
	if len(written) != 1 || written[0] != "a.txt" {
		t.Fatalf("executed writes = %v, want a.txt (auto auto-approves the writer fallback)", written)
	}
}

// TestApplyHeadlessApprovalModeYoloBypassesAskRule confirms only bypass runs an
// explicitly ask-gated command unattended.
func TestApplyHeadlessApprovalModeYoloBypassesAskRule(t *testing.T) {
	prompts, written := runHeadlessWriteOnce(t, ToolApprovalYolo, []string{"write_file"})
	if prompts != 0 {
		t.Fatalf("approval prompts = %d, want 0", prompts)
	}
	if len(written) != 1 || written[0] != "a.txt" {
		t.Fatalf("executed writes = %v, want a.txt (bypass runs even explicit ask rules)", written)
	}
}

// TestApplyHeadlessApprovalModeDontAskDeniesWithoutPrompting checks that dontAsk
// denies a would-ask tool through the non-blocking deny approver rather than
// emitting a prompt or hanging.
func TestApplyHeadlessApprovalModeDontAskDeniesWithoutPrompting(t *testing.T) {
	writer := &recordingWriter{}
	reg := tool.NewRegistry()
	reg.Add(writer)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "write_file", `{"path":"a.txt"}`),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	prompts := 0
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, []string{"write_file"}, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
			}
		}),
	})
	c.ApplyHeadlessApprovalMode(ToolApprovalDontAsk)

	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), "edit", "edit") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurnWithRaw: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("headless dontAsk must not block")
	}
	if prompts != 0 {
		t.Fatalf("approval prompts = %d, want 0 (dontAsk denies silently)", prompts)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.paths) != 0 {
		t.Fatalf("executed writes = %v, want none (dontAsk must deny)", writer.paths)
	}
}

// TestBuildHeadlessApprovalGateMatchesParentExecutorContract pins boot's single
// construction point for every headless-only sub-agent gate (task,
// writer-capable skill runners, the planner) to the identical mode contract
// ApplyHeadlessApprovalMode installs on the parent executor. Before this fix,
// boot always built the mode-unaware default gate (nil approver, ask resolves
// to allow) for those surfaces regardless of the CLI-selected headless
// approval mode, so a task sub-agent could run a write_file call an explicit
// ask rule was supposed to deny under auto. runSubagentGateWriteOnce drives a
// write_file tool call through a gate exactly the way TaskTool.runSubSession
// does — a plain agent.New(...).Run(...) with the gate on agent.Options — so
// this exercises the same mechanism a real sub-agent uses, not a mock.
func TestBuildHeadlessApprovalGateMatchesParentExecutorContract(t *testing.T) {
	runSubagentGateWriteOnce := func(t *testing.T, mode string) []string {
		t.Helper()
		writer := &recordingWriter{}
		reg := tool.NewRegistry()
		reg.Add(writer)
		prov := &scriptedTurns{turns: [][]provider.Chunk{
			toolCallTurn("c1", "write_file", `{"path":"a.txt"}`),
			textTurn("Done."),
		}}
		gate := BuildHeadlessApprovalGate(permission.New("ask", nil, []string{"write_file"}, nil), mode)
		ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{Gate: gate}, event.Discard)

		done := make(chan error, 1)
		go func() { done <- ag.Run(context.Background(), "edit") }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("sub-agent gate in %s mode must not block", mode)
		}
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return append([]string(nil), writer.paths...)
	}

	if got := runSubagentGateWriteOnce(t, ToolApprovalAuto); len(got) != 0 {
		t.Fatalf("auto: executed writes = %v, want none (a sub-agent must fail closed on an explicit ask rule too)", got)
	}
	if got := runSubagentGateWriteOnce(t, ToolApprovalYolo); len(got) != 1 || got[0] != "a.txt" {
		t.Fatalf("yolo: executed writes = %v, want [a.txt] (bypass runs even explicit ask rules)", got)
	}
}

// TestSetToolApprovalModePropagatesToSubagentGate pins the interactive
// counterpart of the boot.Build sub-agent gate fix: a runtime mode switch
// (Shift+Tab -> SetToolApprovalMode) must reach sub-agents too, not just
// refreshInteractiveGate's parent executor gate. Before this fix, boot
// captured the sub-agent gate once at construction (mode-unaware default:
// ask resolves to allow) and SetToolApprovalMode never touched it, so a task
// sub-agent stayed on the boot-time default even after the user switched to
// auto. subagentGate here stands in for what a task/skill/planner sub-agent
// actually reads (boot wires the same *SharedHeadlessGate into all of them).
func TestSetToolApprovalModePropagatesToSubagentGate(t *testing.T) {
	policy := permission.New("ask", nil, []string{"write_file"}, nil)
	subagentGate := NewSharedHeadlessGate(policy, ToolApprovalAsk)
	c := New(Options{Policy: policy, SubagentGate: subagentGate})

	runSubagentWriteOnce := func(t *testing.T) []string {
		t.Helper()
		writer := &recordingWriter{}
		reg := tool.NewRegistry()
		reg.Add(writer)
		prov := &scriptedTurns{turns: [][]provider.Chunk{
			toolCallTurn("c1", "write_file", `{"path":"a.txt"}`),
			textTurn("Done."),
		}}
		ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{Gate: subagentGate}, event.Discard)
		done := make(chan error, 1)
		go func() { done <- ag.Run(context.Background(), "edit") }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("sub-agent gate must not block")
		}
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return append([]string(nil), writer.paths...)
	}

	// Fresh sub-agent gate at the default Ask posture: no UI to prompt
	// through, so the explicit ask rule resolves to allow — the existing,
	// intentional headless contract for a never-switched session.
	if got := runSubagentWriteOnce(t); len(got) != 1 || got[0] != "a.txt" {
		t.Fatalf("ask (initial): executed writes = %v, want [a.txt]", got)
	}

	c.SetToolApprovalMode(ToolApprovalAuto)
	if got := runSubagentWriteOnce(t); len(got) != 0 {
		t.Fatalf("auto: executed writes = %v, want none (sub-agent gate must follow the mode switch and fail closed on the ask rule)", got)
	}

	c.SetToolApprovalMode(ToolApprovalYolo)
	if got := runSubagentWriteOnce(t); len(got) != 1 || got[0] != "a.txt" {
		t.Fatalf("yolo: executed writes = %v, want [a.txt] (bypass runs even explicit ask rules)", got)
	}
}

// TestInteractiveGateIgnoresSessionAllowForFreshHumanTools guards the memory
// contract: --allowed-tools (SessionAllow) must never satisfy a tool that
// requires fresh human approval every call, even though SessionAllow outranks Ask
// rules for ordinary tools.
func TestInteractiveGateIgnoresSessionAllowForFreshHumanTools(t *testing.T) {
	policy := permission.New("ask", nil, nil, nil).
		WithSessionAllow([]string{"remember", "forget", "write_file"})
	c := New(Options{Policy: policy})

	gate := c.newInteractiveGate()
	for _, name := range []string{memoryRememberTool, memoryForgetTool} {
		if got := gate.Policy.DecideSubject(name, false, ""); got != permission.Ask {
			t.Fatalf("%s decision = %v, want Ask (SessionAllow must not cover fresh-human tools)", name, got)
		}
	}
	// An ordinary tool in the allowlist is still honored.
	if got := gate.Policy.DecideSubject("write_file", false, ""); got != permission.Allow {
		t.Fatalf("write_file decision = %v, want Allow (SessionAllow still applies to ordinary tools)", got)
	}
}
