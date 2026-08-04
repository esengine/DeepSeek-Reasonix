package permission

import (
	"encoding/json"
	"testing"
)

func bashArgs(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGitTagIsReadOnlyOnlyWhenListing(t *testing.T) {
	readOnly := []string{"git tag", "git tag -l", "git tag --list", "git tag -l 'v1.*'", "git tag --sort=-creatordate"}
	for _, cmd := range readOnly {
		if !BashCommandIsReadOnly(bashArgs(t, cmd)) {
			t.Errorf("%q only lists tags and should stay read-only", cmd)
		}
	}
	writes := []string{"git tag v1.2.3", "git tag -d v1.2.3", "git tag -a v1.2.3 -m release", "git tag --delete v1.2.3"}
	for _, cmd := range writes {
		if BashCommandIsReadOnly(bashArgs(t, cmd)) {
			t.Errorf("%q writes the ref namespace and must not count as read-only", cmd)
		}
	}
}

func TestDestructiveGitCommandsAreWarned(t *testing.T) {
	for _, cmd := range []string{
		"git restore src/main.go",
		"git restore --staged .",
		"git checkout -- src/main.go",
		"git stash drop",
		"git stash clear",
	} {
		if BashDangerWarning(cmd) == "" {
			t.Errorf("%q can destroy uncommitted work and should carry a warning", cmd)
		}
	}
	for _, cmd := range []string{"git status", "git checkout -b feature", "git stash list"} {
		if w := BashDangerWarning(cmd); w != "" {
			t.Errorf("%q is not destructive but was labelled %q", cmd, w)
		}
	}
}

func TestDestructiveGitCommandsGetNoPrefixGrant(t *testing.T) {
	if got := BashCommandPrefix("git restore src/main.go"); got != "" {
		t.Errorf("a session grant for git restore must stay exact, got prefix %q", got)
	}
	if got := BashCommandPrefix("git status --short"); got == "" {
		t.Error("ordinary read-only git commands should still earn a prefix grant")
	}
}
