package permission

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		{"Bash(npm run build)", "Bash", "npm run build", false, true},
		{"Edit(docs/**)", "Edit", "docs/**", false, true},
		{"bash(rm -rf*)", "bash", "rm -rf*", false, true},
		{"  read_file  ", "read_file", "", false, true},
		{"bash( go test ./... )", "bash", " go test ./... ", false, true}, // subject preserved verbatim
		{"bash(echo (hi))", "bash", "echo (hi)", false, true},             // first '(' wins, trailing ')'
		{"bash=rm *.log", "bash", "rm *.log", true, true},                 // literal: '*' is not a wildcard
		{"bash=make FOO=bar", "bash", "make FOO=bar", true, true},         // split on first '=' only
		{"bash=echo (hi)", "bash", "echo (hi)", true, true},               // '=' before '(' → literal, parens kept
		{"bash(make FOO=*)", "bash", "make FOO=*", false, true},           // '(' before '=' → still a glob
		{"get-user", "get-user", "", false, true},
		{"Set-Content", "Set-Content", "", false, true},
		{"set-content", "set-content", "", false, true},
		{"git", "git", "", false, true},
		{"Get-CustomThing", "Get-CustomThing", "", false, true},
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

func TestPowerShellLikeBareToolNamesKeepGenericRuleSemantics(t *testing.T) {
	p := New("ask",
		[]string{"get-user"},
		[]string{"write-report"},
		[]string{"set-profile"},
	)
	if got := p.DecideSubject("get-user", false, ""); got != Allow {
		t.Fatalf("bare allow tool rule = %v, want Allow", got)
	}
	if got := p.DecideSubject("write-report", true, ""); got != Ask {
		t.Fatalf("bare ask tool rule = %v, want Ask", got)
	}
	if got := p.DecideSubject("set-profile", true, ""); got != Deny {
		t.Fatalf("bare deny tool rule = %v, want Deny", got)
	}
	if got := p.DecideSubject("bash", false, "get-user --all"); got != Ask {
		t.Fatalf("hyphenated command inherited a bare tool allow = %v, want Ask", got)
	}
	cmdletAllow := New("ask", []string{"Set-Content"}, nil, nil)
	if got := cmdletAllow.DecideSubject("Set-Content", false, ""); got != Allow {
		t.Fatalf("bare cmdlet allow tool rule = %v, want Allow", got)
	}
	if got := cmdletAllow.DecideSubject("bash", false, "Set-Content app.go"); got != Ask {
		t.Fatalf("bare cmdlet allow leaked into Bash = %v, want Ask", got)
	}
	legacy := New("allow", nil, nil, []string{"Set-Content"})
	if got := legacy.DecideSubject("Set-Content", true, ""); got != Deny {
		t.Fatalf("legacy cmdlet exact tool deny = %v, want Deny", got)
	}
	if got := legacy.DecideSubject("bash", false, "set-content app.go"); got != Deny {
		t.Fatalf("legacy cmdlet Bash deny = %v, want Deny", got)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"rm -rf*", "rm -rf /tmp/x", true}, // '*' crosses '/'
		{"go test*", "go test ./...", true},
		{"rm *", "rm *.log", true},
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

func TestSubjectsForMoveFile(t *testing.T) {
	got := Subjects(json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"secrets/a.md"}`))
	want := []string{"tmp/a.md", "secrets/a.md"}
	if len(got) != len(want) {
		t.Fatalf("Subjects length = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Subjects[%d] = %q, want %q (all subjects: %v)", i, got[i], want[i], got)
		}
	}
	if primary := Subject(json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"secrets/a.md"}`)); primary != "tmp/a.md" {
		t.Fatalf("Subject primary = %q, want source path", primary)
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

func TestPolicyDecideMoveFileChecksBothEndpoints(t *testing.T) {
	denyDest := New("allow", nil, nil, []string{"Edit(secrets/**)"})
	if got := denyDest.Decide("move_file", false, json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"secrets/a.md"}`)); got != Deny {
		t.Fatalf("destination deny rule = %v, want Deny", got)
	}

	askDest := New("allow", nil, []string{"Edit(secrets/**)"}, nil)
	if got := askDest.Decide("move_file", false, json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"secrets/a.md"}`)); got != Ask {
		t.Fatalf("destination ask rule = %v, want Ask", got)
	}

	sourceOnlyAllow := New("ask", []string{"Edit(tmp/**)"}, nil, nil)
	if got := sourceOnlyAllow.Decide("move_file", false, json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"docs/a.md"}`)); got != Ask {
		t.Fatalf("source-only allow = %v, want Ask for unallowed destination", got)
	}

	bothAllowed := New("ask", []string{"Edit(tmp/**)", "Edit(docs/**)"}, nil, nil)
	if got := bothAllowed.Decide("move_file", false, json.RawMessage(`{"source_path":"tmp/a.md","destination_path":"docs/a.md"}`)); got != Allow {
		t.Fatalf("both endpoints allowed = %v, want Allow", got)
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

func TestSessionAllowPrecedence(t *testing.T) {
	p := New("ask", nil, []string{"Edit(docs/**)", "Bash(git *)"}, []string{"Edit(docs/private/**)", "Bash(git push *)"}).
		WithSessionAllow([]string{"Edit(docs/**)", "Bash(git *)", "(malformed)"})

	cases := []struct {
		name string
		tool string
		args string
		want Decision
	}{
		{"session allow overrides configured ask", "write_file", `{"path":"docs/readme.md"}`, Allow},
		{"configured deny overrides session allow", "write_file", `{"path":"docs/private/key.txt"}`, Deny},
		{"bash session allow overrides configured ask", "bash", `{"command":"git status"}`, Allow},
		{"bash deny overrides session allow", "bash", `{"command":"git push origin main"}`, Deny},
		{"malformed session rule is ignored", "write_file", `{"path":"other.txt"}`, Ask},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Decide(tc.tool, false, json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("Decide = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionAllowEvaluatesCompoundBashPerSegment(t *testing.T) {
	p := New("ask", nil, []string{"Bash(git commit *)"}, []string{"Bash(rm *)"}).
		WithSessionAllow([]string{"Bash(git *)", "Bash(go test *)"})

	if got := p.Decide("bash", false, json.RawMessage(`{"command":"git add . && git commit -m test && go test ./..."}`)); got != Allow {
		t.Fatalf("fully session-allowed compound command = %v, want Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"git status && npm publish"}`)); got != Ask {
		t.Fatalf("partially allowed compound command = %v, want Ask", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"git status && rm output.txt"}`)); got != Deny {
		t.Fatalf("compound command containing denied segment = %v, want Deny", got)
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

type policyReasonApprover struct {
	reason string
}

func (a *policyReasonApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	return true, false, nil
}

func (a *policyReasonApprover) ApproveWithPolicyReason(_ context.Context, _, _ string, _ json.RawMessage, reason string) (bool, bool, string, error) {
	a.reason = reason
	return true, false, "", nil
}

func TestGateReportsMatchedPermissionRule(t *testing.T) {
	args := json.RawMessage(`{"command":"git status && git push origin main"}`)
	approver := &policyReasonApprover{}
	askGate := NewGate(New("allow", nil, []string{"Bash(git push:*)"}, nil), approver)
	if allow, _, err := askGate.Check(context.Background(), "bash", args, false); err != nil || !allow {
		t.Fatalf("ask-gated call = allow %v, err %v", allow, err)
	}
	if got, want := approver.reason, "Matched permission rule: ask Bash(git push:*)"; got != want {
		t.Fatalf("approval reason = %q, want %q", got, want)
	}

	denyGate := NewGate(New("allow", nil, nil, []string{"Bash(git push:*)"}), nil)
	allow, reason, err := denyGate.Check(context.Background(), "bash", args, false)
	if err != nil || allow {
		t.Fatalf("deny-gated call = allow %v, err %v", allow, err)
	}
	if !strings.Contains(reason, "Matched permission rule: deny Bash(git push:*)") {
		t.Fatalf("deny reason = %q, want matched rule", reason)
	}
}

func TestMatchedRuleDoesNotReportAskRuleOverriddenForOneEndpoint(t *testing.T) {
	p := New("ask", nil, []string{"Edit(src/**)"}, nil).
		WithSessionAllow([]string{"Edit(src/**)"})
	args := json.RawMessage(`{"source_path":"src/old.go","destination_path":"generated/new.go"}`)
	if got := p.Decide("move_file", false, args); got != Ask {
		t.Fatalf("move decision = %v, want Ask from uncovered destination fallback", got)
	}
	if rule, ok := p.MatchedRule("move_file", Ask, args); ok {
		t.Fatalf("MatchedRule = %q, want no rule provenance for fallback Ask", rule)
	}
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
	// "Always allow" is tool-wide: the persisted rule is the bare tool name, not
	// pinned to "go build", so any later command runs without re-prompting.
	if remembered != "bash" {
		t.Errorf("remembered rule = %q, want tool-wide %q", remembered, "bash")
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

func TestClaudeStyleRuleMatchesExactCommandWithoutWildcard(t *testing.T) {
	p := New("ask", []string{"Bash(go build)"}, nil, nil)

	if got := p.Decide("bash", false, json.RawMessage(`{"command":"go build"}`)); got != Allow {
		t.Errorf("exact command = %v, want Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"go build ./cmd"}`)); got == Allow {
		t.Errorf("exact command rule matched longer command")
	}
}

// TestLegacyLiteralRuleMatchesExactly guards configs written before the
// Claude-style Bash(...) rules: a literal "bash=rm *.log" must allow only that
// exact command, never the wildcard expansion a glob "bash(rm *.log)" would
// have matched.
func TestLegacyLiteralRuleMatchesExactly(t *testing.T) {
	p := New("ask", []string{"bash=rm *.log"}, nil, nil)

	if got := p.Decide("bash", false, json.RawMessage(`{"command":"rm *.log"}`)); got != Allow {
		t.Errorf("exact command = %v, want Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"rm secrets.log"}`)); got == Allow {
		t.Errorf("literal rule wildcard-matched %q — '*' must stay literal", "rm secrets.log")
	}
}
