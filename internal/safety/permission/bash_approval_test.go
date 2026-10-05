package permission

import (
	"encoding/json"
	"testing"
)

func TestBashSubjectRequiresExplicitApproval(t *testing.T) {
	tests := []struct {
		name      string
		subject   string
		wantHuman bool
		wantExact bool
	}{
		{name: "plain static command", subject: "git status --short"},
		{name: "static compound command", subject: "git status && npm test"},
		{name: "safe null redirect", subject: "git status 2>/dev/null"},
		{name: "simple sudo command", subject: "sudo chmod 644 file"},
		{name: "non-indirect builtin", subject: "builtin printf '%s\\n' ok"},
		{name: "command substitution", subject: "git status $(touch /tmp/x)", wantHuman: true, wantExact: true},
		{name: "backtick substitution", subject: "git status `touch /tmp/x`", wantHuman: true, wantExact: true},
		{name: "process substitution input", subject: "diff <(touch /tmp/x) expected", wantHuman: true, wantExact: true},
		{name: "process substitution output", subject: "tee >(touch /tmp/x)", wantHuman: true, wantExact: true},
		{name: "parameter expansion", subject: "git diff $REV", wantExact: true},
		{name: "arithmetic expansion", subject: "echo $((1 + 1))", wantExact: true},
		{name: "brace expansion", subject: "printf '%s\\n' {a,b}", wantExact: true},
		{name: "extended glob", subject: "printf '%s\\n' @(a|b)", wantExact: true},
		{name: "environment assignment", subject: "REV=HEAD git diff", wantExact: true},
		{name: "env wrapper assignment", subject: "env REV=HEAD git diff", wantExact: true},
		{name: "file redirect", subject: "git status > status.txt", wantExact: true},
		{name: "unquoted glob", subject: "rm *.log", wantExact: true},
		{name: "heredoc", subject: "cat <<EOF\nhello\nEOF", wantExact: true},
		{name: "heredoc nested execution", subject: "cat <<EOF\n$(touch /tmp/x)\nEOF", wantHuman: true, wantExact: true},
		{name: "eval", subject: `eval "touch /tmp/x"`, wantHuman: true, wantExact: true},
		{name: "source", subject: "source ./script.sh", wantHuman: true, wantExact: true},
		{name: "dot source", subject: ". ./script.sh", wantHuman: true, wantExact: true},
		{name: "builtin eval", subject: `builtin eval "touch /tmp/x"`, wantHuman: true, wantExact: true},
		{name: "builtin source", subject: "builtin source ./script.sh", wantHuman: true, wantExact: true},
		{name: "command resolves a name", subject: "command -v uv"},
		{name: "command describes a name", subject: "command -V pytest"},
		{name: "command chained resolves", subject: "command -v uv; command -v pytest"},
		{name: "command runs the program it names", subject: "command git status"},
		{name: "command default path runs the program", subject: "command -p git status"},
		{name: "command eval", subject: `command eval "touch /tmp/x"`, wantHuman: true, wantExact: true},
		{name: "command after option terminator", subject: "command -- git status"},
		{name: "bare command", subject: "command", wantHuman: true, wantExact: true},
		{name: "bash command string", subject: `bash -lc "touch /tmp/x"`, wantHuman: true, wantExact: true},
		{name: "wrapped bash command string", subject: `env bash -c "touch /tmp/x"`, wantHuman: true, wantExact: true},
		{name: "powershell command string", subject: `pwsh -Command "New-Item x"`, wantHuman: true, wantExact: true},
		{name: "cmd command string", subject: `cmd /c "echo x > file"`, wantHuman: true, wantExact: true},
		{name: "python inline code", subject: `python3 -c "open('x','w').close()"`, wantHuman: true, wantExact: true},
		{name: "node inline code", subject: `node -e "require('fs').writeFileSync('x','')"`, wantHuman: true, wantExact: true},
		{name: "node attached inline code", subject: `node --eval="require('fs').writeFileSync('x','')"`, wantHuman: true, wantExact: true},
		{name: "ruby attached inline code", subject: `ruby -eFile.write('x','')`, wantHuman: true, wantExact: true},
		{name: "cmd attached command string", subject: `cmd /cecho x`, wantHuman: true, wantExact: true},
		{name: "find exec", subject: `find . -exec touch {} ;`, wantHuman: true, wantExact: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BashSubjectRequiresExplicitApproval(tt.subject); got != tt.wantHuman {
				t.Errorf("BashSubjectRequiresExplicitApproval(%q) = %v, want %v", tt.subject, got, tt.wantHuman)
			}
			if got := bashSubjectRequiresExactRule(tt.subject); got != tt.wantExact {
				t.Errorf("bashSubjectRequiresExactRule(%q) = %v, want %v", tt.subject, got, tt.wantExact)
			}
		})
	}
}

func TestPowerShellCmdletDenyPrefixIsCaseInsensitive(t *testing.T) {
	p := New("allow", nil, nil, []string{
		"Set-Content",
		"Bash(Add-Content:*)",
		"Bash(Out-File:*)",
	})
	for _, command := range []string{
		`set-content -LiteralPath app.go -Value bad`,
		`ADD-CONTENT -LiteralPath app.go -Value bad`,
		`out-file -FilePath app.go`,
	} {
		if got := p.DecideSubject("bash", false, command); got != Deny {
			t.Fatalf("DecideSubject(%q) = %v, want Deny", command, got)
		}
	}
	if got := p.DecideSubject("bash", false, `Set-Location src`); got != Allow {
		t.Fatalf("unrelated PowerShell command = %v, want Allow", got)
	}
}

func TestPolicyDynamicBashRequiresExplicitApproval(t *testing.T) {
	const command = "git status $(touch /tmp/reasonix-permission-bypass)"

	tests := []struct {
		name string
		p    Policy
		want Decision
	}{
		{name: "writer fallback allow cannot bypass", p: New("allow", nil, nil, nil), want: Ask},
		{name: "explicit dynamic fallback opt-in", p: New("allow", nil, nil, nil).WithAllowDynamicBashFallback(true), want: Allow},
		{name: "dynamic opt-in still requires allow fallback", p: New("ask", nil, nil, nil).WithAllowDynamicBashFallback(true), want: Ask},
		{name: "dynamic opt-in keeps ask precedence", p: New("allow", nil, []string{"Bash(git*)"}, nil).WithAllowDynamicBashFallback(true), want: Ask},
		{name: "dynamic opt-in keeps deny precedence", p: New("allow", nil, nil, []string{"Bash(git*)"}).WithAllowDynamicBashFallback(true), want: Deny},
		{name: "bare allow cannot bypass", p: New("ask", []string{"Bash"}, nil, nil), want: Ask},
		{name: "ordinary glob cannot bypass", p: New("ask", []string{"Bash(git*)"}, nil, nil), want: Ask},
		{name: "legacy prefix cannot bypass", p: New("ask", []string{"Bash(git *)"}, nil, nil), want: Ask},
		{name: "session glob cannot bypass", p: New("ask", nil, nil, nil).WithSessionAllow([]string{"Bash(git*)"}), want: Ask},
		{name: "explicit ask remains ask", p: New("allow", []string{"Bash"}, []string{"Bash(git*)"}, nil), want: Ask},
		{name: "raw deny wins", p: New("allow", []string{"Bash"}, nil, []string{"Bash(git*)"}), want: Deny},
		{name: "scoped raw deny wins", p: New("allow", []string{"Bash"}, nil, []string{"Bash(git status:*)"}), want: Deny},
		{name: "scoped raw ask remains ask", p: New("allow", []string{"Bash"}, []string{"Bash(git status:*)"}, nil), want: Ask},
		{name: "literal allow matches exactly", p: New("ask", []string{"Bash=" + command}, nil, nil), want: Allow},
		{name: "legacy exact allow matches exactly", p: New("ask", []string{"Bash(" + command + ")"}, nil, nil), want: Allow},
		{name: "literal session grant matches exactly", p: New("ask", nil, []string{"Bash(git*)"}, nil).WithSessionAllow([]string{"Bash=" + command}), want: Allow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.DecideSubject("bash", false, command); got != tt.want {
				t.Fatalf("DecideSubject(%q) = %v, want %v", command, got, tt.want)
			}
		})
	}
}

func TestPolicyRawBashPrefixMatchesDynamicSpacing(t *testing.T) {
	command := "git  status $(touch /tmp/x)"
	if got := New("allow", nil, nil, []string{"Bash(git status:*)"}).DecideSubject("bash", false, command); got != Deny {
		t.Fatalf("scoped deny with dynamic spacing = %v, want Deny", got)
	}
}

func TestPolicyDynamicBashShapesRequireExplicitApproval(t *testing.T) {
	p := New("allow", []string{"Bash"}, nil, nil)
	for _, command := range []string{
		"git status `touch /tmp/x`",
		"diff <(touch /tmp/x) expected",
		"tee >(touch /tmp/x)",
		`eval "touch /tmp/x"`,
		"source ./script.sh",
		`builtin eval "touch /tmp/x"`,
		"builtin source ./script.sh",
		`bash -c "touch /tmp/x"`,
		`python3 -c "open('x','w').close()"`,
	} {
		if got := p.DecideSubject("bash", true, command); got != Ask {
			t.Errorf("DecideSubject(%q) = %v, want Ask", command, got)
		}
	}
}

func TestPolicyExactOnlyBashUsesFallbackWithoutReusableAllow(t *testing.T) {
	for _, command := range []string{
		"git diff $REV",
		"echo $((1 + 1))",
		"REV=HEAD git diff",
		"env REV=HEAD git diff",
		"git status > status.txt",
		"rm *.log",
		"cat <<EOF\nhello\nEOF",
	} {
		if got := New("ask", []string{"Bash"}, nil, nil).DecideSubject("bash", false, command); got != Ask {
			t.Errorf("ask fallback for %q = %v, want Ask", command, got)
		}
		if got := New("allow", []string{"Bash"}, nil, nil).DecideSubject("bash", false, command); got != Allow {
			t.Errorf("auto fallback for %q = %v, want Allow", command, got)
		}
		if got := New("deny", []string{"Bash"}, nil, nil).DecideSubject("bash", false, command); got != Deny {
			t.Errorf("deny fallback for %q = %v, want Deny", command, got)
		}
		if got := New("ask", []string{"Bash=" + command}, nil, nil).DecideSubject("bash", false, command); got != Allow {
			t.Errorf("exact literal for %q = %v, want Allow", command, got)
		}
	}
}

func TestPolicyStaticBashRulesRemainReusable(t *testing.T) {
	tests := []struct {
		rule    string
		command string
	}{
		{rule: "Bash(git status:*)", command: "git status --short"},
		{rule: "Bash(git *)", command: "git status --short"},
		{rule: "Bash(git*)", command: "git status --short"},
		{rule: "Bash", command: "git status --short"},
	}
	for _, tt := range tests {
		p := New("ask", []string{tt.rule}, nil, nil)
		if got := p.DecideSubject("bash", false, tt.command); got != Allow {
			t.Errorf("rule %q command %q = %v, want Allow", tt.rule, tt.command, got)
		}
	}
}

func TestDynamicBashRuleMatchingAndCoverage(t *testing.T) {
	const command = "git status $(touch /tmp/x)"
	if RuleMatchesString("Bash(git*)", "bash", command) {
		t.Fatal("broad session allow matched dynamic command")
	}
	if !RuleMatchesString("Bash="+command, "bash", command) {
		t.Fatal("literal session allow did not match exact dynamic command")
	}
	if RuleCoversString("Bash(git*)", "Bash="+command) {
		t.Fatal("broad glob covered dynamic literal rule")
	}
	if RuleCoversString("Bash", "Bash="+command) {
		t.Fatal("bare Bash rule covered dynamic literal rule")
	}
	if !RuleCoversString("Bash="+command, "Bash="+command) {
		t.Fatal("identical dynamic literal rules were not deduplicated")
	}
	if !RuleCoversString("Bash", "Bash") {
		t.Fatal("identical bare rules were not deduplicated")
	}
}

func TestDynamicBashRememberedAsLiteral(t *testing.T) {
	commands := []string{
		"git status $(touch /tmp/x)",
		"rm *.log",
		`eval "touch /tmp/x"`,
	}
	for _, command := range commands {
		want := "Bash=" + command
		if got := RememberRuleForScope("bash", command); got != want {
			t.Errorf("RememberRuleForScope(%q) = %q, want %q", command, got, want)
		}
		if got := SessionGrantRuleForScope("bash", command); got != want {
			t.Errorf("SessionGrantRuleForScope(%q) = %q, want %q", command, got, want)
		}
	}
}

// An interpreter reading a here-document carries its program in the call, the
// same way -c does. The parser reports the redirect, so this is a fact about
// the command rather than a guess about what the interpreter will read; a
// here-document feeding a non-interpreter is ordinary data.
func TestHereDocFedInterpreterIsInlineCode(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    BashApprovalBlocker
	}{
		{"python3 - <<'EOF'\nimport os\nEOF", BashApprovalBlockerInlineCode},
		{"python3 <<'EOF'\nprint(1)\nEOF", BashApprovalBlockerInlineCode},
		{"bash <<'EOF'\nls\nEOF", BashApprovalBlockerInlineCode},
		{"node <<'EOF'\nconsole.log(1)\nEOF", BashApprovalBlockerInlineCode},
		{"cat <<'EOF'\nplain text\nEOF", BashApprovalBlockerNone},
		// The redirect is just as real one level down; a caller that prefixes
		// its call with cd, or puts it in a loop, is not hiding anything.
		{"cd /tmp/x && python3 - <<'EOF'\nimport os\nEOF", BashApprovalBlockerInlineCode},
		{"for f in *; do python3 - <<'EOF'\nx\nEOF\ndone", BashApprovalBlockerInlineCode},
		{"sort <<'EOF'\nb\na\nEOF", BashApprovalBlockerNone},
		{"python3 script.py", BashApprovalBlockerNone},

		// A here-document writing a file is neither inline code nor unreadable:
		// the parser reports it, and the caller needs to hear "write_file",
		// which is advice the unparsable message cannot give.
		{"cd /w && cat > pkg/x.go <<'EOF'\npackage pkg\nEOF", BashApprovalBlockerHereDocBody},
		{"mkdir -p pkg && cat > pkg/x.go <<'EOF'\npackage pkg\nEOF", BashApprovalBlockerHereDocBody},
		{"for f in *.go; do rm $f; done", BashApprovalBlockerUnparsable},
	} {
		if got := BashSubjectApprovalBlocker(tc.command); got != tc.want {
			t.Errorf("%q blocker = %v, want %v", firstLineOfCommand(tc.command), got, tc.want)
		}
	}
}

// Expressing batch inspection as a loop should not by itself require a human.
// The parser names every command a compound statement would run, so when all of
// them are read-only there is nothing left to rule on — and anything the parser
// cannot fully read keeps needing approval.
func TestReadOnlyCompoundStatementsNeedNoApproval(t *testing.T) {
	for _, tc := range []struct {
		command string
		allowed bool
	}{
		{`for f in pkg/*.py; do echo "=== $f ==="; cat "$f"; done`, true},
		{`if [ -f x.py ]; then cat x.py; fi`, true},
		{`for d in */; do ls "$d"; done`, true},

		{`for f in *.py; do rm "$f"; done`, false},              // a writer leaf
		{`for f in *.py; do cat "$f" > out.txt; done`, false},   // redirect creates a file
		{`for f in *; do eval "$f"; done`, false},               // runs what the argv does not name
		{`for f in *; do python3 -c "print(1)"; done`, false},   // inline code inside the loop
		{`for f in $(ls); do cat "$f"; done`, false},            // command substitution
		{`while read l; do echo "$l"; done < input.txt`, false}, // stdin redirect
		{`for f in *; do "$f"; done`, false},                    // the program itself is dynamic
		{`for f in *; do cat "$f"; done; rm -rf /tmp/x`, false}, // a writer after the loop
		{`if true; then git push; fi`, false},                   // external action
		{`for f in *.go; do gofmt -w "$f"; done`, false},        // -w rewrites in place
	} {
		allowed := BashSubjectApprovalBlocker(tc.command) == BashApprovalBlockerNone
		if allowed != tc.allowed {
			t.Errorf("%q allowed = %v, want %v", tc.command, allowed, tc.allowed)
		}
	}
}

func firstLineOfCommand(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i] + " …"
		}
	}
	return s
}

// The three layers that see a bash call must agree about a read-only compound
// statement: the approval blocker (needs a human?), the approval class (can it
// become a rule?), and the read-only classification the policy decides on.
// Disagreement is what left `for f in *.py; do cat "$f"; done` blocked even
// after the blocker stopped objecting to it.
func TestReadOnlyCompoundAgreesAcrossApprovalAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		command  string
		readOnly bool
		decision Decision
	}{
		{`cd /tmp/x && for f in pkg/*.py; do echo "== $f =="; cat "$f"; done`, true, Allow},
		{`for f in *.py; do cat "$f"; done`, true, Allow},
		{`if [ -f x ]; then cat x; fi`, true, Allow},
		{`for f in *.py; do rm "$f"; done`, false, Ask},
		{`for f in *; do cat "$f" > out; done`, false, Ask},
	} {
		args, err := json.Marshal(map[string]string{"command": tc.command})
		if err != nil {
			t.Fatal(err)
		}
		readOnly := BashCommandIsReadOnly(args)
		if readOnly != tc.readOnly {
			t.Errorf("%q readOnly = %v, want %v", tc.command, readOnly, tc.readOnly)
			continue
		}
		if got := New("auto", nil, nil, nil).Decide("bash", readOnly, args); got != tc.decision {
			t.Errorf("%q decision = %v, want %v", tc.command, got, tc.decision)
		}
	}
}
