package permission

import (
	"strings"
	"testing"
)

// TestUnmatchableRulesReportsBareShellCommands covers the misconfiguration in
// #6692: the rules were typed as bare commands, so every one of them named a
// tool that does not exist and the deny list never fired.
func TestUnmatchableRulesReportsBareShellCommands(t *testing.T) {
	got := UnmatchableRules(
		[]string{"git status", "Bash(git diff:*)"},
		[]string{"git push --force"},
		[]string{"git checkout HEAD --"},
	)
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(got), got)
	}
	want := []UnmatchableRule{
		{List: "allow", Rule: "git status", Suggestion: "Bash(git status:*)"},
		{List: "ask", Rule: "git push --force", Suggestion: "Bash(git push --force:*)"},
		{List: "deny", Rule: "git checkout HEAD --", Suggestion: "Bash(git checkout HEAD --:*)"},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestUnmatchableRulesLeavesWorkingRulesAlone is the guardrail that keeps the
// warning trustworthy: a rule that does match must never be reported, or the
// notice trains its reader to ignore it.
func TestUnmatchableRulesLeavesWorkingRulesAlone(t *testing.T) {
	ok := []string{
		"Bash(git status:*)",
		"Bash",
		"edit_file",
		"edit_file(src/**)",
		"mcp__github__create_issue",
		"Bash(git commit -m *)",
		"read_file=exact/path.go",
		"",
	}
	if got := UnmatchableRules(ok, nil, nil); len(got) != 0 {
		t.Fatalf("working rules were reported as unmatchable: %+v", got)
	}
}

// TestUnmatchableRulesKeepsAnExistingSubject checks the suggestion does not
// guess twice: a rule that already carries a subject keeps it.
func TestUnmatchableRulesKeepsAnExistingSubject(t *testing.T) {
	got := UnmatchableRules(nil, nil, []string{"git push(--force)"})
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(got), got)
	}
	if want := "Bash(git push --force)"; got[0].Suggestion != want {
		t.Fatalf("suggestion = %q, want %q", got[0].Suggestion, want)
	}
}

// TestUnmatchableRuleSuggestionsParseBack closes the loop: every suggestion
// the warning prints must itself be a rule that parses and targets bash.
func TestUnmatchableRuleSuggestionsParseBack(t *testing.T) {
	for _, r := range UnmatchableRules([]string{"git status"}, []string{"rm -rf /"}, []string{"git stash drop"}) {
		rule, ok := ParseRule(r.Suggestion)
		if !ok {
			t.Errorf("suggestion %q does not parse", r.Suggestion)
			continue
		}
		if canonicalRuleTool(rule.Tool) != "bash" {
			t.Errorf("suggestion %q targets %q, want the bash tool", r.Suggestion, rule.Tool)
		}
		if !strings.Contains(r.Suggestion, "Bash(") {
			t.Errorf("suggestion %q is not in Bash(...) form", r.Suggestion)
		}
	}
}

// TestUnmatchableRulesCatchesEveryWhitespace keeps the check and the invariant
// it rests on asking the same question: a tool name broken across lines is as
// unmatchable as one with a plain space.
func TestUnmatchableRulesCatchesEveryWhitespace(t *testing.T) {
	for _, raw := range []string{"git\tstatus", "git\nstatus", "git status"} {
		if got := UnmatchableRules(nil, nil, []string{raw}); len(got) != 1 {
			t.Errorf("UnmatchableRules(%q) returned %d findings, want 1", raw, len(got))
		}
	}
}
