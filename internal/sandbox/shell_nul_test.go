package sandbox

import "testing"

func TestNormalizeNulRedirect(t *testing.T) {
	const bash = "/dev/null"
	cases := []struct {
		in, sink, want string
	}{
		{"echo hi 2>nul", bash, "echo hi 2>/dev/null"},
		{"echo hi 2>nul", "$null", "echo hi 2>$null"},
		{"build >nul 2>&1", bash, "build >/dev/null 2>&1"},
		{"a 2>nul; b", bash, "a 2>/dev/null; b"},
		{"go test 1>>NUL", bash, "go test 1>>/dev/null"},
		{"x > nul", bash, "x >/dev/null"},
		{"x >nul", "$null", "x >$null"},
		{"x > null", bash, "x >/dev/null"},
		{"x >null", "$null", "x >$null"},
		{"x 2> /dev/null", "$null", "x 2>$null"},
		{"x 2> \"null\"", "$null", "x 2>$null"},
		{"x 2> '/dev/null'", bash, "x 2>/dev/null"},
		{"x *> null", "$null", "x *>$null"},
		{"probe &>nul", bash, "probe &>/dev/null"},
		// Redirect at the end of a backtick command substitution: the trailing
		// delimiter is the backtick, which used to fall outside the matcher and
		// leak "nul" through unrewritten (#4252 regression). Both `...` and
		// $(...) forms are exercised, across all three targets (nul/null/
		// /dev/null) since they share one trailing-delimiter class.
		{"x=`where git 2>nul`", bash, "x=`where git 2>/dev/null`"},
		{"x=`where git 2>null`", bash, "x=`where git 2>/dev/null`"},
		{"x=`where git 2>/dev/null`", bash, "x=`where git 2>/dev/null`"},
		{"x=$(where git 2>nul)", bash, "x=$(where git 2>/dev/null)"},
		{"x=$(where git 2>null)", bash, "x=$(where git 2>/dev/null)"},
		{"x=$(where git 2>/dev/null)", bash, "x=$(where git 2>/dev/null)"},
		{"x=`where git 2>nul`", "$null", "x=`where git 2>$null`"},
		{"x=`where git 2>null`", "$null", "x=`where git 2>$null`"},
		{"x=`where git 2>/dev/null`", "$null", "x=`where git 2>$null`"},
		// Not a nul redirect — leave untouched.
		{"echo nul", bash, "echo nul"},
		{"grep nul file.txt", bash, "grep nul file.txt"},
		{"cat nul.txt", bash, "cat nul.txt"},
		{"cat null.txt", bash, "cat null.txt"},
		{"run 2>&1", bash, "run 2>&1"},
		{"rm nul", bash, "rm nul"},
		{"rm null", bash, "rm null"},
		{"echo nullish", bash, "echo nullish"},
		// Quoted "2>nul" is a literal argument, not a redirect — left alone.
		{"echo '2>nul'", bash, "echo '2>nul'"},
		{"echo '2>null '", "$null", "echo '2>null '"},
		{"echo \"2>nul;\"", bash, "echo \"2>nul;\""},
	}
	for _, c := range cases {
		if got := normalizeNulRedirect(c.in, c.sink); got != c.want {
			t.Errorf("normalizeNulRedirect(%q, %q) = %q, want %q", c.in, c.sink, got, c.want)
		}
	}
}

func TestArgvNormalizesNulRedirect(t *testing.T) {
	bashArgv := Shell{Kind: ShellBash, Path: "bash"}.argv("echo hi 2>nul")
	if last := bashArgv[len(bashArgv)-1]; last != "echo hi 2>/dev/null" {
		t.Errorf("bash argv command = %q, want nul rewritten to /dev/null", last)
	}
	psArgv := Shell{Kind: ShellPowerShell, Path: "powershell"}.argv("echo hi 2>nul")
	if last := psArgv[len(psArgv)-1]; last != psUTF8Prologue+"echo hi 2>$null" {
		t.Errorf("powershell argv command = %q, want nul rewritten to $null", last)
	}
}
