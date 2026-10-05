package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

// The contract internal/contract/tool enforces over the built-ins, over the tools this
// package owns. Listed explicitly because these are constructed by the host
// rather than registered globally — a new one has to be added here, which is
// the point at which its author is asked for the reason and for which host
// fact it names.
func TestAgentContextualToolsExplainThemselves(t *testing.T) {
	want := map[string]string{
		"submit_plan":         codeNoPlanningTurn,
		"conclude_no_changes": codeNoPlanningTurn,
	}
	for _, target := range []tool.Tool{&SubmitPlanTool{}, &ConcludeNoChangesTool{}} {
		if _, ok := target.(tool.ContextualTool); !ok {
			t.Errorf("%s is listed here but is not contextual", target.Name())
			continue
		}
		r, ok := target.(tool.ContextualReasoner)
		if !ok {
			t.Errorf("%s cannot say why it is unavailable", target.Name())
			continue
		}
		refusal := r.Unavailable(context.Background())
		if strings.TrimSpace(refusal.Message) == "" {
			t.Errorf("%s returned a blank reason", target.Name())
		}
		if refusal.Code != want[target.Name()] {
			t.Errorf("%s refusal code = %q, want %q", target.Name(), refusal.Code, want[target.Name()])
		}
	}
}

// Both plan tools refuse for one predicate and phrase it for their own caller.
// That the words differ while the identity does not is the whole point: a
// reader grouping these two by text would see two kinds of failure.
func TestPlanToolsShareOneIdentityAndKeepTheirOwnWords(t *testing.T) {
	submit := (&SubmitPlanTool{}).Unavailable(context.Background())
	conclude := (&ConcludeNoChangesTool{}).Unavailable(context.Background())
	if submit.Code != conclude.Code {
		t.Fatalf("codes diverged: %q vs %q", submit.Code, conclude.Code)
	}
	if submit.Message == conclude.Message {
		t.Fatal("the two tools now say the same sentence; the test no longer proves words are free to differ")
	}
}
