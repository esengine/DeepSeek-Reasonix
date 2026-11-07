package agent

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

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
	got := supersedeStandingState(kept, nil)

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
	got := supersedeStandingState(kept, nil)

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

// The verbatim tail is newer than every turn the fold keeps, so a copy it
// restates supersedes the kept one even when the kept slice holds that block
// only once.
func TestSupersedeSeesTheCopiesTheTailAlreadyCarries(t *testing.T) {
	const ws = "<workspace>\nCurrent workspace: \"/w\".\n</workspace>"
	kept := []provider.Message{{Role: provider.RoleUser, Content: ws + "\n\nolder"}}
	tail := []provider.Message{
		{Role: provider.RoleAssistant, Content: ws + " quoted by the model"},
		{Role: provider.RoleUser, Content: ws + "\n\nnewer"},
	}
	got := supersedeStandingState(kept, tail)
	if strings.Contains(got[0].Content, "<workspace>") || !strings.Contains(got[0].Content, "older") {
		t.Fatalf("the kept copy survived beside the tail's newer one:\n%q", got[0].Content)
	}
	if tail[1].Content != ws+"\n\nnewer" {
		t.Fatalf("the tail was rewritten: %q", tail[1].Content)
	}

	alone := supersedeStandingState([]provider.Message{{Role: provider.RoleUser, Content: ws + "\n\nonly"}},
		[]provider.Message{{Role: provider.RoleAssistant, Content: ws}})
	if !strings.Contains(alone[0].Content, "<workspace>") {
		t.Fatalf("a block only an assistant message repeats displaced the user turn's copy:\n%q", alone[0].Content)
	}
}
