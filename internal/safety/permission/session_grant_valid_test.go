package permission

import "testing"

func TestEveryRuleTheHostRecordsIsAValidSessionGrant(t *testing.T) {
	for _, c := range [][2]string{
		{"bash", "go test ./..."},
		{"bash", "npm run build"},
		{"bash", "rm -rf /tmp/x"},
		{"bash", "echo hi | wc -l"},
		{"edit_file", "a.go"},
		{"write_file", "b.go"},
		{"web_fetch", "https://example.com/a"},
		{"browser_open", "https://example.com"},
		{"computer_act", "Calculator"},
		{"mcp__files__read", ""},
	} {
		rule := SessionGrantRuleForScope(c[0], c[1])
		if !ValidSessionGrantRule(rule) {
			t.Errorf("host rule %q for %v was rejected", rule, c)
		}
	}
}

func TestARuleTheHostCouldNotHaveWrittenIsNotAValidSessionGrant(t *testing.T) {
	for _, rule := range []string{
		"", "   ", "*", "Bash(*)", "Bash(*:*)", "Bash(rm:*)", "Bash(go:*)", "Bash(rm -rf:*)", "Bash(go test:*", "Bash(go test)",
		"Bash(go test:*)\n", "Bash=", "bash*", "Edit(*)", "tool=", "=x", "a b", "Bash(sudo make:*)",
	} {
		if ValidSessionGrantRule(rule) {
			t.Errorf("%q was accepted as a session grant", rule)
		}
	}
}
