package sandbox

import "testing"

func TestNormalizeNullRedirects(t *testing.T) {
	const bash = "/dev/null"
	cases := []struct {
		in, sink, want string
	}{
		{"echo hi 2>nul", bash, "echo hi 2>/dev/null"},
		{"echo hi 2>nul", "$null", "echo hi 2>$null"},
		{"echo hi 2>/dev/null", "$null", "echo hi 2>$null"},
		{"echo hi 2>$null", bash, "echo hi 2>/dev/null"},
		{"echo hi 2>$NULL", "$null", "echo hi 2>$null"},
		{"build >nul 2>&1", bash, "build >/dev/null 2>&1"},
		{"a 2>nul; b", bash, "a 2>/dev/null; b"},
		{"go test 1>>NUL", bash, "go test 1>>/dev/null"},
		{"x > nul", bash, "x >/dev/null"},
		{"x >nul", "$null", "x >$null"},
		{"probe &>nul", bash, "probe &>/dev/null"},
		{"probe &>/dev/null", "$null", "probe &>$null"},
		{"probe &>>$null", bash, "probe &>>/dev/null"},
		// Not a nul redirect — leave untouched.
		{"echo nul", bash, "echo nul"},
		{"grep nul file.txt", bash, "grep nul file.txt"},
		{"cat nul.txt", bash, "cat nul.txt"},
		{"cat /dev/null.txt", "$null", "cat /dev/null.txt"},
		{"echo '$null >nul '", bash, "echo '$null >nul '"},
		{"echo \"quoted >/dev/null \"", "$null", "echo \"quoted >/dev/null \""},
		{"echo \\>nul", bash, "echo \\>nul"},
		{"run 2>&1", bash, "run 2>&1"},
		{"rm nul", bash, "rm nul"},
		{"echo nullish", bash, "echo nullish"},
	}
	for _, c := range cases {
		if got := normalizeNullRedirects(c.in, c.sink); got != c.want {
			t.Errorf("normalizeNullRedirects(%q, %q) = %q, want %q", c.in, c.sink, got, c.want)
		}
	}
}

func TestNormalizeNullRedirectsPreservesHereDocBody(t *testing.T) {
	in := "cat > out.go <<'EOF'\n" +
		"func main() {\n" +
		"\tdata := []byte(`{\"token\":\"TOKEN_EXAMPLE\"}`)\n" +
		"\tjson.Unmarshal(data, &v)\n" +
		"}\n" +
		"EOF\n"
	if got := normalizeNullRedirects(in, "/dev/null"); got != in {
		t.Fatalf("normalizeNullRedirects changed heredoc body:\n--- got ---\n%s\n--- want ---\n%s", got, in)
	}
}

func TestArgvNormalizesNullRedirects(t *testing.T) {
	bashArgv := Shell{Kind: ShellBash, Path: "bash"}.argv("echo hi 2>nul")
	if last := bashArgv[len(bashArgv)-1]; last != "echo hi 2>/dev/null" {
		t.Errorf("bash argv command = %q, want nul rewritten to /dev/null", last)
	}
	psArgv := Shell{Kind: ShellPowerShell, Path: "powershell"}.argv("echo hi 2>/dev/null")
	if last := psArgv[len(psArgv)-1]; last != psUTF8Prologue+"echo hi 2>$null" {
		t.Errorf("powershell argv command = %q, want /dev/null rewritten to $null", last)
	}
}
