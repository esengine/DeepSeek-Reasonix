package permission

import (
	"encoding/json"
	"testing"
)

// wrapperVerdicts lists each command's verdict against the deny rule
// Bash(rm:*) under mode allow, so any Allow is a command no rule or guard
// stopped. Rows marked unchanged keep the verdict they had before wrapper
// peeling existed.
var wrapperVerdicts = []struct {
	cmd       string
	want      Decision
	unchanged bool
}{
	{"rm -rf b", Deny, true},
	{"env X=1 rm -rf b", Deny, false},
	{"env -i rm b", Deny, false},
	{`env "X=1" rm b`, Deny, false},
	{`env 'X=1' rm b`, Deny, false},
	{`env "X"=1 rm b`, Deny, false},
	{`env "X"='1' rm b`, Deny, false},
	{`env "X"=$HOME rm b`, Deny, false},
	{`env "X=$HOME" rm b`, Ask, false},
	{"env a-b=1 rm b", Deny, false},
	{"env 1X=1 rm b", Deny, false},
	{"env foo.bar=1 rm b", Deny, false},
	{`env "A B=1" rm b`, Deny, false},
	{"env =1 rm b", Ask, false},
	{"sudo a-b=1 rm b", Ask, false},
	{"sudo 1X=1 rm b", Ask, false},
	{`sudo "A B=1" rm b`, Ask, false},
	{`sudo 'X=1' rm b`, Deny, false},
	{`sudo "X=1" rm b`, Deny, false},
	{"env --unset", Ask, true},
	{"env -u X -C /tmp rm b", Deny, false},
	{"env X=$(id) rm b", Deny, false},
	{"env -S 'rm b'", Ask, true},
	{"env $V rm b", Ask, true},
	{"sudo rm -rf b", Deny, false},
	{"sudo -u root rm b", Deny, false},
	{"sudo -u $U rm b", Deny, false},
	{"sudo -s rm b", Ask, true},
	{"sudo -i", Ask, true},
	{"doas rm b", Deny, false},
	{"doas -u root rm b", Deny, false},
	{"command rm b", Deny, false},
	{"command -p rm b", Deny, false},
	{"command -v rm", Allow, true},
	{"builtin rm b", Deny, false},
	{"exec rm b", Deny, false},
	{"nohup rm b", Deny, false},
	{"nohup rm b &", Deny, false},
	{"nohup --frob rm b", Ask, false},
	{"time rm b", Deny, false},
	{"time -p rm b", Deny, false},
	{"/usr/bin/time rm b", Deny, false},
	{"nice rm b", Deny, false},
	{"nice -n 5 rm b", Deny, false},
	{"nice -5 rm b", Deny, false},
	{"nice $CMD b", Ask, false},
	{"! rm b", Deny, false},
	{"/bin/rm b", Deny, false},
	{"./rm b", Deny, false},
	{"'/bin/rm' b", Deny, false},
	{"C:/tools/rm.exe b", Deny, false},
	{`'C:\tools\rm.exe' b`, Deny, false},
	{"rm.exe b", Deny, false},
	{`"rm" b`, Deny, true},
	{`r\m b`, Deny, true},
	{"X=1 rm b", Deny, true},
	{"X=1 sudo rm b", Deny, false},
	{"sudo env X=1 rm b", Deny, false},
	{"env sudo nohup rm b", Deny, false},
	{"echo hi; rm b", Deny, true},
	{"echo hi && sudo rm b", Deny, false},
	{"true || nohup rm b", Deny, false},
	{"ls | env rm b", Deny, false},
	{"rm b > /dev/null", Deny, true},
	{"sudo rm b 2>&1", Deny, false},
	{"RM b", Allow, true},
	{"(rm b)", Ask, true},
	{"{ rm b; }", Ask, true},
	{"( sudo rm b )", Ask, true},
	{"if true; then rm b; fi", Ask, true},
	{"xargs rm", Ask, true},
	{"echo b | xargs rm", Ask, true},
	{"find . -exec rm {} +", Ask, true},
	{"find . -delete", Allow, true},
	{"cat <<EOF\nrm b\nEOF", Allow, true},
	{"cat <<EOF | sudo rm b\nx\nEOF", Ask, true},
	{"echo $(rm b)", Ask, true},
	{"$(echo rm) b", Ask, true},
	{"$CMD b", Ask, true},
	{"sh -c 'rm b'", Ask, true},
	{"timeout 5 rm b", Allow, true},
	{"setsid rm b", Allow, true},
	{"sudo git push", Allow, true},
}

func TestWrappedProgramMatchesDenyRule(t *testing.T) {
	for _, tc := range wrapperVerdicts {
		t.Run(tc.cmd, func(t *testing.T) {
			p := New("allow", nil, nil, []string{"Bash(rm:*)"})
			if got := p.DecideSubject("bash", false, tc.cmd); got != tc.want {
				t.Errorf("deny rule, mode allow: %q = %v, want %v", tc.cmd, got, tc.want)
			}
			askMode := New("ask", nil, nil, []string{"Bash(rm:*)"})
			wantAsk := Ask
			if tc.want == Deny {
				wantAsk = Deny
			}
			if got := askMode.DecideSubject("bash", false, tc.cmd); got != wantAsk {
				t.Errorf("deny rule, mode ask: %q = %v, want %v", tc.cmd, got, wantAsk)
			}
		})
	}
}

func TestWrappedProgramMatchesAskRule(t *testing.T) {
	for _, tc := range wrapperVerdicts {
		t.Run(tc.cmd, func(t *testing.T) {
			p := New("allow", nil, []string{"Bash(rm:*)"}, nil)
			want := tc.want
			if want == Deny {
				want = Ask
			}
			if got := p.DecideSubject("bash", false, tc.cmd); got != want {
				t.Errorf("ask rule, mode allow: %q = %v, want %v", tc.cmd, got, want)
			}
		})
	}
}

func TestWrapperDenyRuleNamesTheWrapperToo(t *testing.T) {
	p := New("allow", nil, nil, []string{"Bash(sudo:*)"})
	for _, cmd := range []string{"sudo rm b", "env sudo ls", "/usr/bin/sudo -n ls", "nohup sudo ls"} {
		if got := p.DecideSubject("bash", false, cmd); got != Deny {
			t.Errorf("%q = %v, want Deny: a rule on the wrapper itself still matches", cmd, got)
		}
	}
	if got := p.DecideSubject("bash", false, "ls"); got != Allow {
		t.Errorf("ls = %v, want Allow", got)
	}
}

func TestMultiWordDenyRuleSeesThroughWrappers(t *testing.T) {
	p := New("allow", []string{"Bash(git:*)"}, nil, []string{"Bash(git push:*)"})
	cases := map[string]Decision{
		"git push origin main":            Deny,
		"env GIT_TRACE=1 git push":        Deny,
		"sudo -u deploy git push --force": Deny,
		"/usr/bin/git push":               Deny,
		"git status":                      Allow,
		"nohup git pull":                  Allow,
	}
	for cmd, want := range cases {
		if got := p.DecideSubject("bash", false, cmd); got != want {
			t.Errorf("%q = %v, want %v", cmd, got, want)
		}
	}
}

// Peeling is for refusing. An allow rule covers the command it names and does
// not extend to a wrapped or path-spelled form of it.
func TestPeelingNeverWidensAllowRules(t *testing.T) {
	p := New("ask", []string{"Bash(git:*)", "Bash(npm test:*)"}, nil, nil)
	cases := map[string]Decision{
		"git status":          Allow,
		"git push origin":     Allow,
		"sudo git push":       Ask,
		"env X=1 git push":    Ask,
		"command git push":    Ask,
		"nohup git push":      Ask,
		"time git push":       Ask,
		"nice git push":       Ask,
		"exec git push":       Ask,
		"/usr/bin/git push":   Ask,
		"timeout 30 npm test": Ask,
		"npm test":            Allow,
		"nice npm test":       Ask,
		"env CI=1 npm test":   Ask,
	}
	for cmd, want := range cases {
		if got := p.DecideSubject("bash", false, cmd); got != want {
			t.Errorf("%q = %v, want %v", cmd, got, want)
		}
	}
	exact := New("ask", []string{"bash=sudo git push"}, nil, nil)
	if got := exact.DecideSubject("bash", false, "sudo git push"); got != Allow {
		t.Errorf("an exact rule for the wrapped form must still allow it, got %v", got)
	}
	session := New("ask", nil, nil, nil).WithSessionAllow([]string{"Bash(git:*)"})
	if got := session.DecideSubject("bash", false, "sudo git push"); got != Ask {
		t.Errorf("a session grant for git must not cover sudo git, got %v", got)
	}
}

// A deny or ask rule outranks an allow rule on the peeled command exactly as
// it does on the raw one.
func TestPeeledDenyBeatsAllowRule(t *testing.T) {
	p := New("allow", []string{"Bash(rm:*)", "Bash(sudo:*)"}, nil, []string{"Bash(rm -rf:*)"})
	if got := p.DecideSubject("bash", false, "sudo rm -rf /"); got != Deny {
		t.Errorf("sudo rm -rf = %v, want Deny", got)
	}
}

func TestWrappedProgramRuleProvenance(t *testing.T) {
	args := json.RawMessage(`{"command":"nohup /bin/rm -rf b"}`)
	p := New("allow", nil, nil, []string{"Bash(rm:*)"})
	if got := p.Decide("bash", false, args); got != Deny {
		t.Fatalf("Decide = %v, want Deny", got)
	}
	if rule, ok := p.MatchedRule("bash", Deny, args); !ok || rule != "Bash(rm:*)" {
		t.Errorf("MatchedRule = %q, %v, want Bash(rm:*)", rule, ok)
	}
	if !p.ExplicitlyDenies("bash", args) {
		t.Error("ExplicitlyDenies = false for a wrapped denied program")
	}
	ask := New("allow", nil, []string{"Bash(rm:*)"}, nil)
	if rule, ok := ask.MatchedRule("bash", Ask, args); !ok || rule != "Bash(rm:*)" {
		t.Errorf("ask MatchedRule = %q, %v, want Bash(rm:*)", rule, ok)
	}
}

func TestOpaqueWrapperIsNeverAllowedByFallback(t *testing.T) {
	for _, mode := range []string{"allow", "ask"} {
		p := New(mode, nil, nil, []string{"Bash(rm:*)"})
		for _, cmd := range []string{"nice $CMD b", "nohup --frob rm b", "doas -s", "sudo --frob ls", "env -S 'ls'"} {
			if got := p.DecideSubject("bash", false, cmd); got == Allow {
				t.Errorf("mode %s: %q = Allow, want a human or a refusal", mode, cmd)
			}
		}
	}
	denyMode := New("deny", nil, nil, nil)
	if got := denyMode.DecideSubject("bash", false, "nice $CMD b"); got != Deny {
		t.Errorf("mode deny: opaque wrapper = %v, want Deny", got)
	}
}

// An expansion inside the quotes makes the word non-static: the program it
// hides cannot be named, so a human decides whatever the mode or rule says.
func TestExpansionInsideQuotedAssignmentNeedsAHuman(t *testing.T) {
	for _, cmd := range []string{`env "X=$HOME" rm b`, `env "$A=1" rm b`, `sudo "X=$HOME" rm b`} {
		for _, mode := range []string{"allow", "ask"} {
			p := New(mode, nil, nil, []string{"Bash(rm:*)"})
			if got := p.DecideSubject("bash", false, cmd); got != Ask {
				t.Errorf("mode %s: %q = %v, want Ask", mode, cmd, got)
			}
		}
	}
}
