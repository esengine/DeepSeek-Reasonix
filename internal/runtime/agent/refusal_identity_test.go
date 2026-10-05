package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

// stubRefusingTool is always out of context and refuses with whatever it is
// given, so the wording and the identity can be moved independently. Any
// classification that tracks the wording shows up here as a test that passes
// for the wrong half.
type stubRefusingTool struct{ refusal tool.Refusal }

func (stubRefusingTool) Name() string                         { return "stub_refuser" }
func (stubRefusingTool) Description() string                  { return "test double" }
func (stubRefusingTool) Schema() json.RawMessage              { return json.RawMessage(`{"type":"object"}`) }
func (stubRefusingTool) ReadOnly() bool                       { return true }
func (stubRefusingTool) ProviderVisible(context.Context) bool { return false }

func (s stubRefusingTool) Unavailable(context.Context) tool.Refusal { return s.refusal }

func (stubRefusingTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

// The identity owns the classification. Reword the refusal and the code the
// host records must not move; change the code under an unchanged sentence and
// it must. Both halves are needed: a reader that happened to key on the words
// would pass the first check alone.
func TestRefusalClassificationFollowsCodeNotWording(t *testing.T) {
	const code = "goal.no_active_turn"
	for _, tc := range []struct {
		name    string
		refusal tool.Refusal
		want    string
	}{
		{
			name:    "the shipped wording",
			refusal: tool.Refusal{Code: code, Message: "update_goal is only available while an active goal turn is running — no goal state was changed"},
			want:    code,
		},
		{
			name:    "reworded, same fact",
			refusal: tool.Refusal{Code: code, Message: "there is currently no active goal turn"},
			want:    code,
		},
		{
			name:    "translated away entirely",
			refusal: tool.Refusal{Code: code, Message: "当前没有正在运行的 goal turn"},
			want:    code,
		},
		{
			name:    "same sentence, different fact",
			refusal: tool.Refusal{Code: "plan.mode_active", Message: "update_goal is only available while an active goal turn is running — no goal state was changed"},
			want:    "plan.mode_active",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome, gated := contextualToolGateOutcome(context.Background(), stubRefusingTool{refusal: tc.refusal}, "stub_refuser")
			if !gated {
				t.Fatal("the gate did not fire on a tool that is never visible")
			}
			if outcome.refusalCode != tc.want {
				t.Fatalf("recorded code = %q, want %q", outcome.refusalCode, tc.want)
			}
			if !outcome.blocked {
				t.Fatal("a refused call was not recorded as blocked")
			}
		})
	}
}

// The model has to be able to cite what the host classified on, so the identity
// travels in the text it actually reads — a code kept only in host-side state
// would leave the model with the sentence and nothing else.
func TestRefusalIdentityReachesTheModelVisibleText(t *testing.T) {
	refusal := tool.Refusal{Code: "goal.no_active_turn", Message: "no active goal turn"}
	outcome, gated := contextualToolGateOutcome(context.Background(), stubRefusingTool{refusal: refusal}, "stub_refuser")
	if !gated {
		t.Fatal("the gate did not fire")
	}
	if !strings.Contains(outcome.output, "goal.no_active_turn") {
		t.Fatalf("model-visible text carries no identity: %q", outcome.output)
	}
	if !strings.Contains(outcome.output, "no active goal turn") {
		t.Fatalf("model-visible text lost the wording: %q", outcome.output)
	}
}

// A contextual tool with nothing to say is itself a distinguishable state, so
// the fallback carries its own identity rather than handing the reader a bare
// sentence to match.
func TestUnexplainedContextualToolStillCarriesAnIdentity(t *testing.T) {
	refusal := unavailableReason(context.Background(), stubRefusingTool{}, "stub_refuser")
	if refusal.Code != "tool.unavailable_unspecified" {
		t.Fatalf("fallback code = %q", refusal.Code)
	}
	if strings.TrimSpace(refusal.Message) == "" {
		t.Fatal("fallback left the model with neither words nor identity")
	}
}
