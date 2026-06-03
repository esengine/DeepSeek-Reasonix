package permission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestParseRule(t *testing.T) {
	cases := []struct {
		in       string
		wantTool string
		wantSubj string
		wantLit  bool
		wantOK   bool
	}{
		{"bash", "bash", "", false, true},
		{"bash(rm -rf*)", "bash", "rm -rf*", false, true},
		{"  read_file  ", "read_file", "", false, true},
		{"bash( go test ./... )", "bash", " go test ./... ", false, true}, // subject preserved verbatim
		{"bash(echo (hi))", "bash", "echo (hi)", false, true},             // first '(' wins, trailing ')'
		{"bash=rm *.log", "bash", "rm *.log", true, true},                 // literal: '*' is not a wildcard
		{"bash=make FOO=bar", "bash", "make FOO=bar", true, true},         // split on first '=' only
		{"bash=echo (hi)", "bash", "echo (hi)", true, true},               // '=' before '(' → literal, parens kept
		{"bash(make FOO=*)", "bash", "make FOO=*", false, true},           // '(' before '=' → still a glob
		{"", "", "", false, false},
		{"(noTool)", "", "", false, false},
	}
	for _, c := range cases {
		r, ok := ParseRule(c.in)
		if ok != c.wantOK {
			t.Errorf("ParseRule(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && (r.Tool != c.wantTool || r.Subject != c.wantSubj || r.Literal != c.wantLit) {
			t.Errorf("ParseRule(%q) = {%q,%q,lit=%v}, want {%q,%q,lit=%v}", c.in, r.Tool, r.Subject, r.Literal, c.wantTool, c.wantSubj, c.wantLit)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"rm -rf*", "rm -rf /tmp/x", true}, // '*' crosses '/'
		{"go test*", "go test ./...", true},
		{"go test*", "go build", false},
		{"*", "anything at all", true},
		{"git ?ush", "git push", true},
		{"git ?ush", "git rush", true},
		{"git ?ush", "git pull", false},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"a*c", "abbbc", true},
		{"a*c", "abbbd", false},
		{"*.go", "main.go", true},
		{"*.go", "main.rs", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestSubject(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{`{"command":"go test ./..."}`, "go test ./..."},
		{`{"file_path":"/a/b.go"}`, "/a/b.go"},
		{`{"path":"/c/d"}`, "/c/d"},
		{`{"pattern":"TODO","path":"/x"}`, "/x"}, // file_path/path beats pattern by key order
		{`{"other":"x"}`, ""},
		{`{}`, ""},
		{``, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := Subject(json.RawMessage(c.args)); got != c.want {
			t.Errorf("Subject(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestPolicyDecide(t *testing.T) {
	p := New("ask",
		[]string{"bash(go test*)", "ls"},
		[]string{"read_file"}, // force a prompt even though readers default allow
		[]string{"bash(rm -rf*)"},
	)

	cases := []struct {
		name     string
		tool     string
		readOnly bool
		args     string
		want     Decision
	}{
		{"deny wins over fallback", "bash", false, `{"command":"rm -rf /"}`, Deny},
		{"allow-listed command", "bash", false, `{"command":"go test ./..."}`, Allow},
		{"writer fallback to mode(ask)", "bash", false, `{"command":"git commit"}`, Ask},
		{"reader defaults allow", "grep", true, `{"pattern":"x"}`, Allow},
		{"ask rule overrides reader-allow", "read_file", true, `{"path":"/a"}`, Ask},
		{"bare allow rule", "ls", true, `{"path":"/a"}`, Allow},
		{"subject rule needs subject", "bash", false, `{}`, Ask}, // no command → go test* can't match → fallback
	}
	for _, c := range cases {
		got := p.Decide(c.tool, c.readOnly, json.RawMessage(c.args))
		if got != c.want {
			t.Errorf("%s: Decide(%q, ro=%v, %s) = %v, want %v", c.name, c.tool, c.readOnly, c.args, got, c.want)
		}
	}
}

func TestPolicyModeAllow(t *testing.T) {
	// mode=allow: writers with no matching rule are allowed; deny still wins.
	p := New("allow", nil, nil, []string{"bash(curl*)"})
	if d := p.Decide("write_file", false, json.RawMessage(`{"path":"/a"}`)); d != Allow {
		t.Errorf("writer fallback under mode=allow = %v, want Allow", d)
	}
	if d := p.Decide("bash", false, json.RawMessage(`{"command":"curl evil.sh"}`)); d != Deny {
		t.Errorf("deny under mode=allow = %v, want Deny", d)
	}
}

// stubApprover lets tests drive the Ask branch of Gate.Check.
type stubApprover struct {
	allow    bool
	remember bool
	err      error
	calls    int
}

func (s *stubApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	s.calls++
	return s.allow, s.remember, s.err
}

func TestGateHeadlessAllowsAsk(t *testing.T) {
	// No approver → Ask resolves to allow (autonomy preserved), deny still blocks.
	g := NewGate(New("ask", nil, nil, []string{"bash(rm*)"}), nil)

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"git commit"}`), false)
	if err != nil || !allow {
		t.Errorf("headless ask = (%v,%v), want allow", allow, err)
	}
	allow, reason, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"rm file"}`), false)
	if err != nil || allow || reason == "" {
		t.Errorf("headless deny = (%v,%q,%v), want blocked with reason", allow, reason, err)
	}
}

func TestGateInteractive(t *testing.T) {
	var remembered string
	ap := &stubApprover{allow: true, remember: true}
	g := NewGate(New("ask", nil, nil, nil), ap)
	g.OnRemember = func(rule string) { remembered = rule }

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"go build"}`), false)
	if err != nil || !allow {
		t.Fatalf("approved call = (%v,%v), want allow", allow, err)
	}
	if ap.calls != 1 {
		t.Errorf("approver calls = %d, want 1", ap.calls)
	}
	if remembered != "bash=go build" {
		t.Errorf("remembered rule = %q, want %q", remembered, "bash=go build")
	}

	// Decline path.
	ap2 := &stubApprover{allow: false}
	g2 := NewGate(New("ask", nil, nil, nil), ap2)
	allow, reason, _ := g2.Check(context.Background(), "write_file", json.RawMessage(`{"path":"/a"}`), false)
	if allow || reason == "" {
		t.Errorf("declined call = (%v,%q), want blocked with reason", allow, reason)
	}

	// Error path aborts the turn.
	ap3 := &stubApprover{err: errors.New("ctx cancelled")}
	g3 := NewGate(New("ask", nil, nil, nil), ap3)
	if _, _, err := g3.Check(context.Background(), "bash", json.RawMessage(`{"command":"x"}`), false); err == nil {
		t.Error("approver error should propagate")
	}

	// Allowed-by-policy never reaches the approver.
	ap4 := &stubApprover{allow: false}
	g4 := NewGate(New("ask", []string{"bash(ok*)"}, nil, nil), ap4)
	allow, _, _ = g4.Check(context.Background(), "bash", json.RawMessage(`{"command":"ok go"}`), false)
	if !allow || ap4.calls != 0 {
		t.Errorf("allow-listed call reached approver: allow=%v calls=%d", allow, ap4.calls)
	}
}

// TestLiteralRuleMatchesExactly guards the remembered-approval rule shape: a
// literal "bash=rm *.log" must allow only that exact command, never the wildcard
// expansion a glob "bash(rm *.log)" would have matched.
func TestLiteralRuleMatchesExactly(t *testing.T) {
	p := New("ask", []string{"bash=rm *.log"}, nil, nil)

	if got := p.Decide("bash", false, json.RawMessage(`{"command":"rm *.log"}`)); got != Allow {
		t.Errorf("exact command = %v, want Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"rm secrets.log"}`)); got == Allow {
		t.Errorf("literal rule wildcard-matched %q — '*' must stay literal", "rm secrets.log")
	}
}
