package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// The user's own words are what no later step can rebuild: the workspace still
// holds the code a constraint governs, nothing holds the constraint. A fold the
// summarizer could not digest has to carry them, shortened, or the model is told
// that earlier messages exist and given none of them.
func TestMechanicalFoldCarriesTheUsersWordsShortened(t *testing.T) {
	long := "never touch the billing module " + strings.Repeat("because audit asked for it ", 40)
	fold := []provider.Message{
		{Role: provider.RoleUser, Content: long},
		{Role: provider.RoleAssistant, Content: "assistant chatter that must not ride along"},
		{Role: provider.RoleTool, Content: "tool output that must not ride along"},
		{Role: provider.RoleUser, Content: "prefer table-driven tests"},
		{Role: provider.RoleUser, Content: "local-only note", LocalOnly: true},
	}
	digest := mechanicalFoldDigest(fold)
	for _, want := range []string{"never touch the billing module", "prefer table-driven tests", "summary was unavailable", "5 earlier message"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest lacks %q:\n%s", want, digest)
		}
	}
	for _, banned := range []string{"assistant chatter", "tool output", "local-only note"} {
		if strings.Contains(digest, banned) {
			t.Errorf("digest carries %q:\n%s", banned, digest)
		}
	}
	if strings.Contains(digest, long) {
		t.Errorf("a long turn rode whole; it must be shortened")
	}
	if strings.Index(digest, "billing") > strings.Index(digest, "table-driven") {
		t.Errorf("the user's turns are not in the order they were said:\n%s", digest)
	}
}

// A user turn reaches the fold as the provider saw it, with the host's date,
// instructions and policy blocks around the person's words. Those blocks are not
// what the user said, and the per-turn cut would spend the line on them.
func TestMechanicalFoldCarriesWhatThePersonSaidNotTheHostBlocks(t *testing.T) {
	prefix := "<current-date>Today's date is 2026-10-21 (Wednesday); the local time zone is UTC+08:00.</current-date>\n" +
		"<project-instructions>" + strings.Repeat("keep the build green ", 30) + "</project-instructions>\n"
	fold := []provider.Message{
		{Role: provider.RoleUser, Content: prefix + "never touch the billing module", RawContent: "never touch the billing module"},
		{Role: provider.RoleUser, Content: prefix + "prefer table-driven tests"},
		{Role: provider.RoleUser, Content: prefix},
	}
	digest := mechanicalFoldDigest(fold)
	for _, want := range []string{"never touch the billing module", "prefer table-driven tests"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest lacks the user's %q:\n%s", want, digest)
		}
	}
	for _, banned := range []string{"Today's date", "keep the build green", "project-instructions"} {
		if strings.Contains(digest, banned) {
			t.Errorf("digest reports the host's %q as the user's words:\n%s", banned, digest)
		}
	}
	if got := strings.Count(digest, "\n- "); got != 2 {
		t.Errorf("listed %d user turns, want 2: a turn holding only host blocks said nothing:\n%s", got, digest)
	}
}

func TestMechanicalFoldStaysBoundedForAHugeNumberOfTurns(t *testing.T) {
	var fold []provider.Message
	for range 3000 {
		fold = append(fold, provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 600)})
	}
	if got := len(mechanicalFoldDigest(fold)); got > mechanicalUserTotalChars+1500 {
		t.Fatalf("digest is %d chars for 3000 turns, want it near the %d cap", got, mechanicalUserTotalChars)
	}
}
