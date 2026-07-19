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

type freshStubApprover struct {
	allow   bool
	reason  string
	calls   int
	subject string
}

type mcpStubApprover struct {
	stubApprover
	mcpCalls    int
	destructive bool
	forced      bool
	reviewer    string
	subject     string
}

func (s *mcpStubApprover) ApproveMCP(_ context.Context, _ string, subject string, _ json.RawMessage, destructive, forced bool, reviewer string) (bool, string, error) {
	s.mcpCalls++
	s.destructive = destructive
	s.forced = forced
	s.reviewer = reviewer
	s.subject = subject
	return s.allow, "reviewed", s.err
}

func (s *freshStubApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	return true, false, nil
}

func (s *freshStubApprover) ApproveFresh(_ context.Context, _ string, subject string, _ json.RawMessage) (bool, string, error) {
	s.calls++
	s.subject = subject
	return s.allow, s.reason, nil
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

func TestGateFreshApprovalIgnoresAllowButHonorsDeny(t *testing.T) {
	ap := &freshStubApprover{allow: true}
	g := NewGate(New("allow", []string{"mcp__srv__danger"}, nil, nil), ap)
	allow, reason, err := g.CheckFresh(context.Background(), "mcp__srv__danger", "srv/danger", json.RawMessage(`{"target":"x"}`), true)
	if err != nil || !allow || reason != "" || ap.calls != 1 || ap.subject != "srv/danger" {
		t.Fatalf("fresh allow = (%v,%q,%v), calls=%d subject=%q", allow, reason, err, ap.calls, ap.subject)
	}

	deniedApprover := &freshStubApprover{allow: true}
	denied := NewGate(New("allow", nil, nil, []string{"mcp__srv__danger"}), deniedApprover)
	allow, reason, err = denied.CheckFresh(context.Background(), "mcp__srv__danger", "srv/danger", nil, false)
	if err != nil || allow || reason == "" || deniedApprover.calls != 0 {
		t.Fatalf("fresh explicit deny = (%v,%q,%v), approver calls=%d", allow, reason, err, deniedApprover.calls)
	}
}

func TestGateFreshApprovalFailsClosedWithoutFreshApprover(t *testing.T) {
	g := NewGate(New("allow", nil, nil, nil), nil)
	allow, reason, err := g.CheckFresh(context.Background(), "mcp__srv__danger", "srv/danger", nil, false)
	if err != nil || allow || !strings.Contains(reason, "fresh human approval") {
		t.Fatalf("headless fresh approval = (%v,%q,%v), want fail closed", allow, reason, err)
	}
}

func TestGateMCPApprovalPrecedence(t *testing.T) {
	args := json.RawMessage(`{"path":"target"}`)
	t.Run("explicit deny beats approve", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
		g := NewGate(New("allow", nil, nil, []string{"mcp__srv__tool"}), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "approve", "auto_review")
		if err != nil || allow || ap.mcpCalls != 0 {
			t.Fatalf("deny result allow=%v err=%v reviewer calls=%d", allow, err, ap.mcpCalls)
		}
	})

	t.Run("destructive beats approve and forwards reviewer", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
		g := NewGate(New("allow", []string{"mcp__srv__tool"}, nil, nil), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, true, true, "approve", "auto_review")
		if err != nil || !allow || ap.mcpCalls != 1 || !ap.destructive || !ap.forced || ap.reviewer != "auto_review" || ap.subject != "srv/tool" {
			t.Fatalf("destructive result allow=%v err=%v approver=%+v", allow, err, ap)
		}
	})

	t.Run("prompt reviews readers and writers", func(t *testing.T) {
		for _, readOnly := range []bool{true, false} {
			ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
			g := NewGate(New("allow", []string{"mcp__srv__tool"}, nil, nil), ap)
			allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, readOnly, false, "prompt", "user")
			if err != nil || !allow || ap.mcpCalls != 1 {
				t.Fatalf("readOnly=%v allow=%v err=%v calls=%d", readOnly, allow, err, ap.mcpCalls)
			}
		}
	})

	t.Run("writes reviews only writers", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
		g := NewGate(New("allow", nil, nil, nil), ap)
		allow, _, _ := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, true, false, "writes", "user")
		if !allow || ap.mcpCalls != 0 {
			t.Fatalf("reader allow=%v calls=%d", allow, ap.mcpCalls)
		}
		allow, _, _ = g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "writes", "user")
		if !allow || ap.mcpCalls != 1 {
			t.Fatalf("writer allow=%v calls=%d", allow, ap.mcpCalls)
		}
	})

	t.Run("approve overrides ordinary ask", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: false}}
		g := NewGate(New("ask", nil, []string{"mcp__srv__tool"}, nil), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "approve", "user")
		if err != nil || !allow || ap.calls != 0 || ap.mcpCalls != 0 {
			t.Fatalf("approve allow=%v err=%v normal=%d mcp=%d", allow, err, ap.calls, ap.mcpCalls)
		}
	})

	t.Run("approve overrides global deny fallback", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: false}}
		g := NewGate(New("deny", nil, nil, nil), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "approve", "user")
		if err != nil || !allow || ap.mcpCalls != 0 {
			t.Fatalf("approve allow=%v err=%v mcp=%d", allow, err, ap.mcpCalls)
		}
	})

	t.Run("auto routes an ask to the configured reviewer", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
		g := NewGate(New("ask", nil, []string{"mcp__srv__tool"}, nil), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "auto", "auto_review")
		if err != nil || !allow || ap.calls != 0 || ap.mcpCalls != 1 || ap.forced {
			t.Fatalf("auto allow=%v err=%v normal=%d mcp=%d", allow, err, ap.calls, ap.mcpCalls)
		}
	})

	t.Run("auto without reviewer preserves legacy approver", func(t *testing.T) {
		ap := &mcpStubApprover{stubApprover: stubApprover{allow: true}}
		g := NewGate(New("ask", nil, []string{"mcp__srv__tool"}, nil), ap)
		allow, _, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", args, false, false, "auto", "")
		if err != nil || !allow || ap.calls != 1 || ap.mcpCalls != 0 {
			t.Fatalf("legacy auto allow=%v err=%v normal=%d mcp=%d", allow, err, ap.calls, ap.mcpCalls)
		}
	})
}

func TestGateMCPConfiguredReviewFailsClosedWithoutReviewer(t *testing.T) {
	g := NewGate(New("allow", nil, nil, nil), nil)
	for _, tc := range []struct {
		mode        string
		destructive bool
	}{
		{mode: "prompt"},
		{mode: "writes"},
		{mode: "approve", destructive: true},
	} {
		allow, reason, err := g.CheckMCP(context.Background(), "mcp__srv__tool", "srv/tool", nil, false, tc.destructive, tc.mode, "user")
		if err != nil || allow || !strings.Contains(reason, "non-interactive") {
			t.Fatalf("mode=%s destructive=%v result=(%v,%q,%v)", tc.mode, tc.destructive, allow, reason, err)
		}
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
