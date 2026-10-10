package permission

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var vocabNames = []string{"bash", "read_file", "write_file", "edit_file", "multi_edit", "grep", "web_fetch", "browser_open", "computer_act"}

func testVocabulary() Vocabulary {
	return Vocabulary{Names: vocabNames, Open: func(n string) bool { return strings.HasPrefix(n, "mcp__") }}
}

func TestVocabularyCheckNamesTheRuleThatReachesNoTool(t *testing.T) {
	v := testVocabulary()
	for _, tc := range []struct {
		list, rule string
		unknown    bool
		tool       string
	}{
		{"ask", "rm", true, "rm"},
		{"deny", "git reset", true, "git reset"},
		{"allow", "Bash(rm", true, "Bash(rm"},
		{"deny", "Read(*.env)", true, "Read"},
		{"ask", "bash", false, ""},
		{"ask", "Bash(rm:*)", false, ""},
		{"deny", "Edit(*.env*)", false, ""},
		{"deny", "file_mutation(*.env*)", false, ""},
		{"allow", "Browser=https://example.com", false, ""},
		{"ask", "mcp__github__create_issue", false, ""},
		{"deny", "Set-Content", false, ""},
		{"allow", "Set-Content", true, "Set-Content"},
		{"allow", "", false, ""},
	} {
		err := v.Check(tc.list, tc.rule)
		if !tc.unknown {
			if err != nil {
				t.Errorf("%s %q: unexpected %v", tc.list, tc.rule, err)
			}
			continue
		}
		var u *UnknownToolError
		if !errors.Is(err, ErrUnknownTool) || !errors.As(err, &u) || u.List != tc.list || u.Rule != tc.rule || u.Tool != tc.tool {
			t.Errorf("%s %q: got %#v, want unknown tool %q", tc.list, tc.rule, err, tc.tool)
		}
	}
}

// A rule the vocabulary calls dormant must change no decision: dropping it from
// the policy leaves every verdict as it was, so flagging it can only be a report.
func TestVocabularyDormantRulesDecideNothing(t *testing.T) {
	v := testVocabulary()
	lists := map[string][]string{
		"allow": {"rm", "Bash(go test:*)", "git status", "Edit"},
		"ask":   {"rm", "git reset", "Bash(git push:*)", "read_file(*.env*)", "Read"},
		"deny":  {"rm", "git reset", "Set-Content", "Bash(rm -rf:*)", "file_mutation(*.env*)", "Write"},
	}
	kept := map[string][]string{}
	var dormant int
	for list, rules := range lists {
		for _, r := range rules {
			if v.Check(list, r) != nil {
				dormant++
				continue
			}
			kept[list] = append(kept[list], r)
		}
	}
	if dormant == 0 {
		t.Fatal("corpus has no dormant rule")
	}
	full := New("ask", lists["allow"], lists["ask"], lists["deny"])
	pruned := New("ask", kept["allow"], kept["ask"], kept["deny"])
	calls := []struct {
		tool string
		args string
	}{
		{"bash", `{"command":"rm x"}`}, {"bash", `{"command":"rm -rf build"}`}, {"bash", `{"command":"git reset --hard"}`},
		{"bash", `{"command":"git status"}`}, {"bash", `{"command":"go test ./..."}`}, {"bash", `{"command":"git push origin main"}`},
		{"bash", `{"command":"Set-Content a.txt hi"}`}, {"bash", `{"command":"ls | wc -l"}`},
		{"write_file", `{"path":".env","content":"x"}`}, {"write_file", `{"path":"a.go","content":"x"}`},
		{"edit_file", `{"path":"a.go"}`}, {"read_file", `{"path":".env"}`}, {"read_file", `{"path":"a.go"}`},
		{"grep", `{"pattern":"x"}`}, {"web_fetch", `{"url":"https://example.com"}`},
	}
	for _, c := range calls {
		args := json.RawMessage(c.args)
		for _, ro := range []bool{true, false} {
			if a, b := full.Decide(c.tool, ro, args), pruned.Decide(c.tool, ro, args); a != b {
				t.Errorf("%s %s readOnly=%v: %v with dormant rules, %v without", c.tool, c.args, ro, a, b)
			}
		}
	}
}
