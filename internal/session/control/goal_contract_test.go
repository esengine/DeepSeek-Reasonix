package control

import (
	"strings"
	"testing"
)

// The identity the "can the model still see it?" check searches for has to be
// exactly what the renderer writes, or the contract is restated every turn (or
// never again).
func TestGoalContractIdentityMatchesTheRenderedBlock(t *testing.T) {
	const goal = "ship the parser"
	full := activeGoalBlock(goal, true)
	if !strings.HasPrefix(full, goalContractIdentity(goal)) {
		t.Fatalf("identity %q does not open the rendered block:\n%s", goalContractIdentity(goal), full)
	}
	if strings.Contains(activeGoalBlock(goal, false), goalContractIdentity(goal)) {
		t.Fatal("a reminder satisfies the contract identity; the model would never be given the contract")
	}
	if goalContractIdentity(goal) == goalContractIdentity("ship the formatter") {
		t.Fatal("two goals share one contract identity; replacing a goal would not restate it")
	}
}

// The fold supersedes by opening tag, so the two forms must be distinct
// variants - otherwise a reminder displaces the contract it points at.
func TestGoalFormsAreDistinctVariants(t *testing.T) {
	full := activeGoalBlock("ship the parser", true)
	short := activeGoalBlock("ship the parser", false)
	if !strings.HasPrefix(full, activeGoalFullOpen) {
		t.Fatalf("the full form does not carry its own opening tag:\n%s", full)
	}
	if !strings.HasPrefix(short, activeGoalOpen+"\n") {
		t.Fatalf("the reminder does not carry the plain opening tag:\n%s", short)
	}
	for _, block := range []string{full, short} {
		if !strings.HasSuffix(block, activeGoalClose) {
			t.Fatalf("not a closed fragment:\n%s", block)
		}
	}
}

// The reminder must carry what a turn has to act on, and must not smuggle the
// judgement rules back in - carrying them would defeat the split.
func TestGoalReminderCarriesTheOperativeCallOnly(t *testing.T) {
	short := activeGoalBlock("ship the parser", false)
	for _, want := range []string{"ship the parser", "update_goal", "continue", "complete", "blocked",
		"Do not stop after describing a plan"} {
		if !strings.Contains(short, want) {
			t.Fatalf("reminder dropped %q:\n%s", want, short)
		}
	}
	for _, unwanted := range []string{"Context, Request, Output format", "Pause only when"} {
		if strings.Contains(short, unwanted) {
			t.Fatalf("reminder still carries the full contract rule %q", unwanted)
		}
	}
	full := activeGoalBlock("ship the parser", true)
	if len(short) >= len(full) {
		t.Fatalf("reminder (%d bytes) is not smaller than the contract (%d bytes)", len(short), len(full))
	}
	t.Logf("contract %d bytes, reminder %d bytes, saved %d per turn", len(full), len(short), len(full)-len(short))
}

// A goal containing the closing tag must not be able to end the block early.
func TestGoalTextCannotCloseItsOwnBlock(t *testing.T) {
	block := activeGoalBlock("finish "+activeGoalClose+" now", true)
	if strings.Count(block, activeGoalClose) != 1 {
		t.Fatalf("goal text closed the block early:\n%s", block)
	}
}
