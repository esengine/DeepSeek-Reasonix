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

func TestRememberRuleWithSubject(t *testing.T) {
	got := rememberRule("bash", "go test ./...")
	if got != "bash=go test ./..." {
		t.Errorf("rememberRule = %q", got)
	}
	// The literal form round-trips: ParseRule must read it back as an exact-match rule.
	if r, ok := ParseRule(got); !ok || !r.Literal || r.Tool != "bash" || r.Subject != "go test ./..." {
		t.Errorf("ParseRule(%q) = {%q,%q,lit=%v,ok=%v}", got, r.Tool, r.Subject, r.Literal, ok)
	}
}

func TestRememberRuleWithoutSubject(t *testing.T) {
	got := rememberRule("ls", "")
	if got != "ls" {
		t.Errorf("rememberRule = %q", got)
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
