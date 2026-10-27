package instruction

import (
	"strings"
	"testing"
)

func TestBlockNamesTheEntriesAFolderTheUserHasNotTrustedSupplied(t *testing.T) {
	docs := []Document{
		{Path: "/home/u/.reasonix/AGENTS.md", Scope: ScopeUser, Body: "mine"},
		{Path: "/repo/AGENTS.md", Scope: ScopeProject, Directory: "/repo", Body: "from the repo", Depth: 0},
		{Path: "/repo/AGENTS.local.md", Scope: ScopeLocal, Directory: "/repo", Body: "local", Depth: 0},
	}
	plain := Block(docs)
	if strings.Contains(plain, "unverified") {
		t.Fatalf("a trusted folder's block carries a provenance warning:\n%s", plain)
	}
	marked := Block(MarkUnverified(docs))
	if strings.Count(marked, "unverified") < 3 {
		t.Fatalf("both project entries and the intro must say so:\n%s", marked)
	}
	if user := marked[strings.Index(marked, "## user/"):strings.Index(marked, "## workspace/")]; strings.Contains(user, "unverified") {
		t.Fatalf("the user's own entry is not the folder's:\n%s", user)
	}
}
