package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
)

// billedThinkingTokens stands in for a provider that charged for thinking whose
// text never arrived — the only shape a replay can actually recover.
const billedThinkingTokens = 64

// TestModelSilentToolCallReasoningIsNotReplayed pins the cost fix: a tool-call
// turn billed zero thinking tokens is ordinary model behaviour between tools,
// so it must neither replay the request nor consume the incident's one replay.
func TestModelSilentToolCallReasoningIsNotReplayed(t *testing.T) {
	prov := toolCallReasoningRequiredProvider{testutil.NewMock("deepseek-proxy")}
	a := New(prov, echoRegistry(), sessionstore.NewSession(""), Options{MissingReasoningWarnStateDir: testenv.TempDir(t)}, event.Discard)
	calls := []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}

	for round := 1; round <= 3; round++ {
		if got := a.observeMissingToolCallReasoning(calls, "", 0); got != reasoningModelSilent {
			t.Fatalf("silent round %d = %v, want %v", round, got, reasoningModelSilent)
		}
	}
	if got := a.observeMissingToolCallReasoning(calls, "", billedThinkingTokens); got != reasoningLostReplay {
		t.Fatalf("lost field after silent rounds = %v, want %v", got, reasoningLostReplay)
	}
}
