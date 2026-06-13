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
		{"probe &>nul", bash, "probe &>/dev/null"},
		// Not a nul redirect — leave untouched.
		{"echo nul", bash, "echo nul"},
		{"grep nul file.txt", bash, "grep nul file.txt"},
		{"cat nul.txt", bash, "cat nul.txt"},
		{"run 2>&1", bash, "run 2>&1"},
		{"rm nul", bash, "rm nul"},
		{"echo nullish", bash, "echo nullish"},
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
