package permission

import (
	"encoding/json"
	"testing"
)

// --- ParseDecision ---

func TestParseDecisionAllow(t *testing.T) {
	if ParseDecision("allow") != Allow {
		t.Error("ParseDecision(\"allow\") should be Allow")
	}
	if ParseDecision("ALLOW") != Allow {
		t.Error("ParseDecision(\"ALLOW\") should be Allow")
	}
	if ParseDecision("  allow  ") != Allow {
		t.Error("ParseDecision with whitespace should be Allow")
	}
}

func TestParseDecisionDeny(t *testing.T) {
	if ParseDecision("deny") != Deny {
		t.Error("ParseDecision(\"deny\") should be Deny")
	}
	if ParseDecision("DENY") != Deny {
		t.Error("ParseDecision(\"DENY\") should be Deny")
	}
}

func TestParseDecisionAsk(t *testing.T) {
	if ParseDecision("ask") != Ask {
		t.Error("ParseDecision(\"ask\") should be Ask")
	}
}

func TestParseDecisionUnknown(t *testing.T) {
	if ParseDecision("unknown") != Ask {
		t.Error("ParseDecision(\"unknown\") should default to Ask")
	}
	if ParseDecision("") != Ask {
		t.Error("ParseDecision(\"\") should default to Ask")
	}
	if ParseDecision("  ") != Ask {
		t.Error("ParseDecision(\"  \") should default to Ask")
	}
}

// --- Decision.String ---

func TestDecisionString(t *testing.T) {
	if Allow.String() != "allow" {
		t.Errorf("Allow.String() = %q", Allow.String())
	}
	if Ask.String() != "ask" {
		t.Errorf("Ask.String() = %q", Ask.String())
	}
	if Deny.String() != "deny" {
		t.Errorf("Deny.String() = %q", Deny.String())
	}
	if Decision(99).String() != "unknown" {
		t.Errorf("unknown Decision.String() = %q", Decision(99).String())
	}
}

// --- matchGlob edge cases ---

func TestMatchGlobEmptyPattern(t *testing.T) {
	// Empty pattern matches empty name (both consumed simultaneously).
	if !matchGlob("", "") {
		t.Error("empty pattern should match empty name")
	}
	if matchGlob("", "anything") {
		t.Error("empty pattern should not match non-empty name")
	}
}

func TestMatchGlobOnlyStars(t *testing.T) {
	if !matchGlob("***", "anything") {
		t.Error("pattern *** should match anything")
	}
	if !matchGlob("*", "") {
		t.Error("pattern * should match empty string")
	}
}

func TestMatchGlobPatternLongerThanName(t *testing.T) {
	if matchGlob("abcdefgh", "abc") {
		t.Error("pattern longer than name should not match")
	}
}

func TestMatchGlobConsecutiveStars(t *testing.T) {
	if !matchGlob("a**c", "abc") {
		t.Error("a**c should match abc")
	}
}

func TestMatchGlobQuestionMark(t *testing.T) {
	if !matchGlob("?", "a") {
		t.Error("? should match single char")
	}
	if matchGlob("?", "") {
		t.Error("? should not match empty")
	}
	if matchGlob("?", "ab") {
		t.Error("? should not match two chars")
	}
}

// --- Subject edge cases ---

func TestSubjectNestedJSON(t *testing.T) {
	// Array values should not match.
	got := Subject(json.RawMessage(`{"command": ["array", "value"]}`))
	if got != "" {
		t.Errorf("array command should return empty, got %q", got)
	}
}

func TestSubjectNullValue(t *testing.T) {
	got := Subject(json.RawMessage(`{"command": null}`))
	if got != "" {
		t.Errorf("null command should return empty, got %q", got)
	}
}

func TestSubjectEmptyCommand(t *testing.T) {
	got := Subject(json.RawMessage(`{"command": ""}`))
	if got != "" {
		t.Errorf("empty command should return empty, got %q", got)
	}
}

func TestSubjectPriority(t *testing.T) {
	// command > file_path > path > pattern
	got := Subject(json.RawMessage(`{"pattern":"pat","path":"/p","file_path":"/f","command":"cmd"}`))
	if got != "cmd" {
		t.Errorf("priority: got %q, want cmd", got)
	}
}

// --- rememberRule ---

func TestRememberRuleWithBashSubjectUsesPrefixWhenAvailable(t *testing.T) {
	// Bash commands with a safe prefix prefer the prefix over the exact command
	// so "always allow" covers similar invocations (e.g. different search terms).
	got := rememberRule("bash", "go test ./...")
	if got != "Bash(go test:*)" {
		t.Errorf("rememberRule = %q, want Bash(go test:*)", got)
	}
	if r, ok := ParseRule(got); !ok || r.Literal || r.Tool != "Bash" || r.Subject != "go test:*" {
		t.Errorf("ParseRule(%q) = {%q,%q,lit=%v,ok=%v}", got, r.Tool, r.Subject, r.Literal, ok)
	}
	// Verify the prefix rule matches similar commands.
	if !RuleMatchesString(got, "bash", "go test ./...") {
		t.Errorf("prefix rule should match the exact command")
	}
	if !RuleMatchesString(got, "bash", "go test ./internal/control") {
		t.Errorf("prefix rule should match similar go test command")
	}
	if RuleMatchesString(got, "bash", "go build ./...") {
		t.Errorf("prefix rule should not match different go subcommand")
	}
}

func TestRememberRuleForBashUsesPrefixWhenAvailable(t *testing.T) {
	got := RememberRuleForScope("bash", "go test ./...")
	if got != "Bash(go test:*)" {
		t.Errorf("RememberRuleForScope prefix = %q", got)
	}
	if !RuleMatchesString(got, "bash", "go test ./internal/control") {
		t.Errorf("prefix rule should match similar go test command")
	}
	if !RuleMatchesString(got, "bash", "go test") {
		t.Errorf("prefix rule should match the base command without extra args")
	}
	if RuleMatchesString(got, "bash", "go build ./...") {
		t.Errorf("prefix rule should not match different go subcommand")
	}
	if RuleMatchesString(got, "bash", "go testing ./...") {
		t.Errorf("prefix rule should not match partial command words")
	}
	if RuleMatchesString(got, "bash", "go test ./... && rm -rf /tmp/x") {
		t.Errorf("prefix rule should not match commands with shell syntax")
	}
	if !RuleMatchesString("Bash(go test *)", "bash", "go test ./legacy") {
		t.Errorf("legacy space-star prefix should still match similar commands")
	}
	if RuleMatchesString("Bash(go test *)", "bash", "go test ./legacy && rm -rf /tmp/x") {
		t.Errorf("legacy space-star prefix should not match commands with shell syntax")
	}
}

func TestRememberRuleWithFileSubjectIsToolWide(t *testing.T) {
	// File mutation tools are remembered tool-wide so "always allow editing"
	// covers any file, matching the session-grant behaviour.
	got := rememberRule("edit_file", "src/app.go")
	if got != "Edit" {
		t.Errorf("rememberRule = %q, want Edit", got)
	}
	if r, ok := ParseRule(got); !ok || r.Literal || r.Tool != "Edit" || r.Subject != "" {
		t.Errorf("ParseRule(%q) = {%q,%q,lit=%v,ok=%v}", got, r.Tool, r.Subject, r.Literal, ok)
	}
}

// TestPersistedEditRuleIsToolWide asserts a deliberate design choice: when a
// user persists an "always allow" for a file-mutation tool, the saved rule is
// "Edit" — tool-wide, with no path restriction.  This means approving one
// edit_file call and choosing "Always allow (save to config)" grants blanket
// edit permission for every file, across sessions, for every file-mutation
// tool (write_file, multi_edit, etc.).  Deny rules still take precedence.
func TestPersistedEditRuleIsToolWide(t *testing.T) {
	rule := RememberRuleForScope("edit_file", "src/app.go")
	if rule != "Edit" {
		t.Fatalf("persisted rule = %q, want tool-wide Edit (no path restriction)", rule)
	}
	// The tool-wide Edit rule matches any file-mutation tool on any file.
	allMutationTools := []string{"write_file", "edit_file", "multi_edit", "notebook_edit", "delete_range", "delete_symbol"}
	for _, tm := range allMutationTools {
		if !RuleMatchesString(rule, tm, "any/path/at/all.txt") {
			t.Errorf("tool-wide Edit should match %s on any path", tm)
		}
	}
	// It must NOT match non-mutation tools (otherwise a denylist would be
	// needed for every tool, which isn't the intent).
	if RuleMatchesString(rule, "bash", "rm -rf /") {
		t.Errorf("tool-wide Edit must not match bash")
	}
}

func TestRememberRuleWithoutSubject(t *testing.T) {
	got := rememberRule("ls", "")
	if got != "ls" {
		t.Errorf("rememberRule = %q", got)
	}
}

func TestSessionGrantKeyScopesBashByCommand(t *testing.T) {
	a := SessionGrantKey("bash", "go build")
	b := SessionGrantKey("bash", "go test ./...")
	if a == b {
		t.Fatalf("bash session grant keys should differ by command: %q", a)
	}
}

func TestSessionGrantKeyGroupsFileMutationTools(t *testing.T) {
	a := SessionGrantKey("edit_file", "src/a.go")
	b := SessionGrantKey("write_file", "src/b.go")
	if a != b {
		t.Fatalf("file mutation session grant keys should match, got %q and %q", a, b)
	}
}

func TestSessionGrantRuleForBashUsesPrefix(t *testing.T) {
	got := SessionGrantRuleForScope("bash", "npm run test -- --watch")
	if got != "Bash(npm run test:*)" {
		t.Errorf("SessionGrantRuleForScope prefix = %q", got)
	}
	if !RuleMatchesString(got, "bash", "npm run test -- src") {
		t.Errorf("prefix session rule should match same package script")
	}
	if RuleMatchesString(got, "bash", "npm run build") {
		t.Errorf("prefix session rule should not match another package script")
	}
}

func TestBashCommandPrefixRejectsShellSyntax(t *testing.T) {
	if got := BashCommandPrefix("go test ./... && rm -rf /tmp/x"); got != "" {
		t.Errorf("BashCommandPrefix with shell syntax = %q, want empty", got)
	}
	if got := BashCommandPrefix("rm -rf /tmp/x"); got != "" {
		t.Errorf("BashCommandPrefix dangerous command = %q, want empty", got)
	}
	if got := BashCommandPrefix("go test ./..."); got != "go test:*" {
		t.Errorf("BashCommandPrefix = %q", got)
	}
}

func TestRuleCoversString(t *testing.T) {
	cases := []struct {
		existing  string
		candidate string
		want      bool
	}{
		{"Bash(go test:*)", "Bash(go test ./...)", true},
		{"Bash(go test *)", "Bash(go test ./...)", true}, // legacy generated prefix
		{"bash(go test *)", "Bash(go test)", true},
		{"bash=go test ./...", "Bash(go test ./...)", true},
		{"Bash(go test *)", "Bash(go test:*)", true}, // existing legacy prefix covers the new shape
		{"Bash(go test:*)", "Bash(go test *)", true}, // new prefix prunes legacy prefix on save
		{"Bash(go test ./...)", "Bash(go test:*)", false},
		{"Edit", "Edit(src/app.go)", true},
		{"file_mutation", "Edit(src/app.go)", true},
		{"Edit(src/app.go)", "Edit", false},
		{"Bash(go test:*)", "Bash(go build ./...)", false},
	}
	for _, c := range cases {
		if got := RuleCoversString(c.existing, c.candidate); got != c.want {
			t.Errorf("RuleCoversString(%q, %q) = %v, want %v", c.existing, c.candidate, got, c.want)
		}
	}
}

func TestFileMutationRuleMatchesMutationToolsByPath(t *testing.T) {
	p := New("ask", []string{"Edit(src/app.go)"}, nil, nil)

	if got := p.Decide("write_file", false, json.RawMessage(`{"path":"src/app.go"}`)); got != Allow {
		t.Errorf("write_file same path = %v, want Allow", got)
	}
	if got := p.Decide("multi_edit", false, json.RawMessage(`{"path":"src/app.go"}`)); got != Allow {
		t.Errorf("multi_edit same path = %v, want Allow", got)
	}
	if got := p.Decide("edit_file", false, json.RawMessage(`{"path":"src/other.go"}`)); got == Allow {
		t.Errorf("edit_file different path = %v, want not Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"cat src/app.go"}`)); got == Allow {
		t.Errorf("bash should not match Edit rule")
	}
}

// --- New ---

func TestNewPolicy(t *testing.T) {
	p := New("deny",
		[]string{"ls"},
		[]string{"read_file"},
		[]string{"bash(rm*)"},
	)
	if p.Mode != Deny {
		t.Errorf("Mode = %v", p.Mode)
	}
	if len(p.Allow) != 1 {
		t.Errorf("Allow count = %d", len(p.Allow))
	}
	if len(p.Ask) != 1 {
		t.Errorf("Ask count = %d", len(p.Ask))
	}
	if len(p.Deny) != 1 {
		t.Errorf("Deny count = %d", len(p.Deny))
	}
}

// --- NewGate ---

func TestNewGate(t *testing.T) {
	p := New("ask", nil, nil, nil)
	g := NewGate(p, nil)
	if g.Policy.Mode != Ask {
		t.Errorf("Policy.Mode = %v", g.Policy.Mode)
	}
	if g.Approver != nil {
		t.Error("Approver should be nil")
	}
}
