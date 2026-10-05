package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func barrierAgent(t *testing.T) *Agent {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	return &Agent{svc: agentServices{tools: reg}}
}

func bashCall(command string) provider.ToolCall {
	args, _ := json.Marshal(map[string]any{"command": command})
	return provider.ToolCall{Name: "bash", Arguments: string(args)}
}

// A command the Windows classifier cannot read is MutationUnknown, and
// ToolCallMutates counts unknown as mutating. A refused one therefore opened a
// barrier that told the model "an earlier modification failed" — and took every
// later change and the verification down with it — when nothing had run at all.
func TestACallStoppedBeforeItRanDoesNotOpenTheBarrier(t *testing.T) {
	a := barrierAgent(t)
	call := bashCall("netsh winhttp show proxy")
	stopped := toolOutcome{
		blocked: true, errMsg: "blocked: preflight",
		execution: &tool.ShellExecution{
			State: tool.ShellStateNotRun, MutationRisk: tool.ShellMutationNotStarted,
		},
	}
	if got := batchFailureBarrier(a, call, stopped); got == barrierFull {
		t.Fatal("a refused unclassifiable command was reported as a failed modification")
	}
}

// It ran, and the host still cannot say whether it wrote. Conservative is the
// only answer left, so this one keeps the barrier.
func TestAnUnclassifiableCommandThatRanAndFailedStillOpensIt(t *testing.T) {
	a := barrierAgent(t)
	ran := toolOutcome{
		errMsg: "error: command exited: exit status 28",
		execution: &tool.ShellExecution{
			State: tool.ShellStateFailed, MutationRisk: tool.ShellMutationMayBePartial,
		},
	}
	if batchFailureBarrier(a, bashCall("curl.exe -I https://example.invalid"), ran) != barrierFull {
		t.Fatal("a command that ran and failed left the rest of the batch unguarded")
	}
}

// A refused command still stops a check. It wrote nothing, so later edits are
// working from a known state — but the check was asked for against a plan one
// step of which never happened, and passing it would mean nothing.
func TestARefusedCommandStillStopsTheCheck(t *testing.T) {
	a := barrierAgent(t)
	args, _ := json.Marshal(map[string]any{"command": "rm -rf build"})
	call := provider.ToolCall{Name: "bash", Arguments: string(args)}
	stopped := toolOutcome{
		blocked: true, errMsg: "blocked: plan mode is read-only",
		execution: &tool.ShellExecution{
			State: tool.ShellStateNotRun, MutationRisk: tool.ShellMutationNotStarted,
		},
	}
	if got := batchFailureBarrier(a, call, stopped); got != barrierVerificationOnly {
		t.Fatalf("barrier = %v, want the one that stops a check and lets independent edits through", got)
	}
}

// A writer the host can name is the case the full barrier exists for: refused
// or not, what follows may have been written against a change that never landed.
func TestARefusedNamedWriterStopsEverything(t *testing.T) {
	a := barrierAgent(t)
	args, _ := json.Marshal(map[string]any{"path": "a.go", "content": "x"})
	call := provider.ToolCall{Name: "write_file", Arguments: string(args)}
	stopped := toolOutcome{blocked: true, errMsg: "blocked: plan mode is read-only"}
	if got := batchFailureBarrier(a, call, stopped); got != barrierFull {
		t.Fatalf("barrier = %v, want everything after a refused writer stopped", got)
	}
}

// The two messages are different claims. Telling a model a modification failed
// when nothing ran sends it looking for a write it never made.
func TestTheMessageSaysWhichOfTheTwoHappened(t *testing.T) {
	full, partial := barrierFull.message(), barrierVerificationOnly.message()
	if !strings.Contains(full, "modification") {
		t.Fatalf("full barrier message = %q", full)
	}
	if strings.Contains(partial, "modification") || !strings.Contains(partial, "before it ran") {
		t.Fatalf("stopped-call message = %q, want it to say nothing ran", partial)
	}
}

// A call that succeeded is not a failure at all.
func TestASuccessfulCallNeverOpensIt(t *testing.T) {
	a := barrierAgent(t)
	if batchFailureBarrier(a, bashCall("netsh winhttp show proxy"), toolOutcome{}) != barrierNone {
		t.Fatal("a successful call opened the barrier")
	}
}
