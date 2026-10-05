package agent

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

const testGoalBlock = "<active-goal>\nship the parser\n\nGoal mode: pursue this goal autonomously.\n</active-goal>"

// Standing state is superseded by the live turn, so retained copies go.
func TestStripSupersededDropsStandingState(t *testing.T) {
	turn := "<reasoning-language>\nthink in zh\n</reasoning-language>\n\n" +
		"<response-language>\nreply in zh\n</response-language>\n\n" +
		testGoalBlock + "\n\n" +
		"<workspace>\nCurrent workspace: \"/repo\".\n</workspace>\n\n" +
		"keep the public API stable"
	got := StripSupersededUserBlocks(turn)
	if got != "keep the public API stable" {
		t.Fatalf("standing state survived the strip:\n%q", got)
	}
}

// A one-time fact is the only record the model has, so it must survive - and
// must not hide a standing block sitting behind it.
func TestStripSupersededKeepsOneTimeFactsAndSeesPastThem(t *testing.T) {
	turn := "<memory-update>\n- prefers tabs\n</memory-update>\n\n" +
		"<background-jobs>\njob 3 finished\n</background-jobs>\n\n" +
		testGoalBlock + "\n\n" +
		"carry on"
	got := StripSupersededUserBlocks(turn)
	for _, want := range []string{"prefers tabs", "job 3 finished", "carry on"} {
		if !strings.Contains(got, want) {
			t.Fatalf("one-time fact %q was dropped:\n%q", want, got)
		}
	}
	if strings.Contains(got, "<active-goal") {
		t.Fatalf("a one-time block ahead of the goal block hid it from the strip:\n%q", got)
	}
}

// The per-turn execution policy records which policy governed that turn, so it
// is provenance rather than standing state.
func TestStripSupersededKeepsPerTurnProvenance(t *testing.T) {
	turn := testGoalBlock + "\n\ndo the thing\n\n<execution-policy preset=\"delivery\" version=\"3\">\nverify=full\n</execution-policy>"
	got := StripSupersededUserBlocks(turn)
	if !strings.Contains(got, "<execution-policy") {
		t.Fatalf("execution-policy provenance was dropped:\n%q", got)
	}
	if strings.Contains(got, "<active-goal") {
		t.Fatalf("goal block survived:\n%q", got)
	}
}

// A synthetic turn that was nothing but standing state must stay a turn.
func TestStripSupersededNeverEmptiesATurn(t *testing.T) {
	if got := StripSupersededUserBlocks(testGoalBlock); got != testGoalBlock {
		t.Fatalf("a turn made only of standing state was emptied:\n%q", got)
	}
}

// Stripping runs on every fold, so it has to reach a fixed point.
func TestStripSupersededIsIdempotent(t *testing.T) {
	turn := testGoalBlock + "\n\nreal user words"
	once := StripSupersededUserBlocks(turn)
	if twice := StripSupersededUserBlocks(once); twice != once {
		t.Fatalf("not a fixed point:\nonce=%q\ntwice=%q", once, twice)
	}
}

func TestStripSupersededLeavesPlainUserTextAlone(t *testing.T) {
	const plain = "look at <active-goal> in input.go and tell me what it does"
	if got := StripSupersededUserBlocks(plain); got != plain {
		t.Fatalf("a tag the user typed was treated as an injection:\n%q", got)
	}
}

// The retention budget must be spent on what the projection carries. Measuring
// the unstripped turn is what let host boilerplate crowd out the user's words:
// the budget here holds every turn once stripped and cannot hold them before.
func TestUserTurnRetentionBudgetHoldsMoreRealTurns(t *testing.T) {
	contract := "<active-goal>\nship the parser\n\n" +
		strings.Repeat("Goal mode: pursue this goal autonomously and honor the task contract. ", 18) +
		"\n</active-goal>"
	const words = "a real constraint the model must not lose"
	region := make([]provider.Message, 0, 12)
	for range 12 {
		region = append(region, provider.Message{Role: provider.RoleUser, Content: contract + "\n\n" + words})
	}

	stripped := fixedTokenEstimate(projectedUserTurnFloor(region[0]))
	unstripped := fixedTokenEstimate(region[0])
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000, compactRatio: defaultCompactRatio}}
	a.budgets.UserTurnKeepTokens = stripped * len(region)
	if unstripped*len(region) <= a.budgets.UserTurnKeepTokens {
		t.Fatalf("fixture is not discriminating: unstripped %d x %d fits the %d budget",
			unstripped, len(region), a.budgets.UserTurnKeepTokens)
	}

	keep := make([]bool, len(region))
	ret := a.window().keepUserTurns(region, keep)
	if ret.Dropped != 0 || ret.Kept != len(region) {
		t.Fatalf("kept %d dropped %d of %d; boilerplate is still charged to the budget",
			ret.Kept, ret.Dropped, len(region))
	}
}

// Standing state is superseded across the retained slice, never per message:
// keptForProjection shapes one message and cannot know what a later one repeats.
func TestKeptForProjectionLeavesStandingStateToTheSupersedePass(t *testing.T) {
	a := &Agent{}
	turn := provider.Message{Role: provider.RoleUser, Content: testGoalBlock + "\n\nwords"}
	if got := a.window().keptForProjection(turn); got.Content != turn.Content {
		t.Fatalf("a single-message pass rewrote standing state: %q", got.Content)
	}
}

// The newest block of each variant survives; older ones of that same variant go.
// A short reminder and the full statement it points at are different variants,
// so the reminder must never displace the contract.
func TestSupersedeKeepsNewestOfEachVariant(t *testing.T) {
	const contract = "<active-goal contract=\"full\">\nship it\n\nthe whole task contract\n</active-goal>"
	const reminder = "<active-goal>\nship it\n\nshort reminder\n</active-goal>"
	kept := []provider.Message{
		{Role: provider.RoleUser, Content: contract + "\n\nfirst"},
		{Role: provider.RoleUser, Content: reminder + "\n\nsecond"},
		{Role: provider.RoleUser, Content: reminder + "\n\nthird"},
		{Role: provider.RoleUser, Content: reminder + "\n\nfourth"},
	}
	got := supersedeStandingState(kept)

	contracts, reminders := 0, 0
	for _, m := range got {
		contracts += strings.Count(m.Content, "the whole task contract")
		reminders += strings.Count(m.Content, "short reminder")
	}
	if contracts != 1 {
		t.Fatalf("the full contract survived %d times, want exactly 1", contracts)
	}
	if reminders != 1 {
		t.Fatalf("reminders collapsed to %d, want exactly 1", reminders)
	}
	if !strings.Contains(got[0].Content, "the whole task contract") {
		t.Fatalf("a reminder displaced the contract it points at:\n%q", got[0].Content)
	}
	if !strings.Contains(got[3].Content, "short reminder") {
		t.Fatalf("the surviving reminder is not the newest one:\n%q", got[3].Content)
	}
	for i, want := range []string{"first", "second", "third", "fourth"} {
		if !strings.Contains(got[i].Content, want) {
			t.Fatalf("message %d lost the user's own words: %q", i, got[i].Content)
		}
	}
}

func TestSeesStandingBlockReadsTheVisibleHistory(t *testing.T) {
	a := &Agent{}
	a.sess.conversation = sessionstore.NewSession("sys")
	if a.SeesStandingBlock("<active-goal contract=\"full\">") {
		t.Fatal("an empty conversation reported a standing block")
	}
	a.sess.conversation.Add(provider.Message{
		Role:    provider.RoleUser,
		Content: "<active-goal contract=\"full\">\nship it\n\ncontract\n</active-goal>\n\ngo",
	})
	if !a.SeesStandingBlock("<active-goal contract=\"full\">") {
		t.Fatal("a block in the visible history was not seen")
	}
	if a.SeesStandingBlock("<active-goal contract=\"other\">") {
		t.Fatal("a different variant matched")
	}
}

// Instructions are re-owed whenever the set changes or a fold summarises the
// turn that carried them, so several copies can be in one retained slice. Only
// the newest states what the rules are now; the rest are a stale duplicate of
// the largest block the turn carries.
func TestSupersedeKeepsOnlyTheNewestInstructions(t *testing.T) {
	const first = "<project-instructions>\n# Instructions\n\nUse tabs.\n</project-instructions>"
	const second = "<project-instructions>\n# Instructions\n\nUse spaces.\n</project-instructions>"
	kept := []provider.Message{
		{Role: provider.RoleUser, Content: first + "\n\nfirst"},
		{Role: provider.RoleUser, Content: "plain turn"},
		{Role: provider.RoleUser, Content: second + "\n\nthird"},
	}
	got := supersedeStandingState(kept)

	if strings.Contains(got[0].Content, "Use tabs.") {
		t.Fatalf("the superseded instructions survived:\n%q", got[0].Content)
	}
	if !strings.Contains(got[2].Content, "Use spaces.") {
		t.Fatalf("the newest instructions did not survive:\n%q", got[2].Content)
	}
	for i, want := range []string{"first", "plain turn", "third"} {
		if !strings.Contains(got[i].Content, want) {
			t.Fatalf("message %d lost its text: %q", i, got[i].Content)
		}
	}
}
