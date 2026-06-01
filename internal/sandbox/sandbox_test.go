package sandbox

import (
	"os/exec"
	"runtime"
	"testing"
)

// --- Spec.enforce ---

func TestEnforce(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false},
		{"off", false},
		{"enforce", true},
		{"Enforce", false}, // case-sensitive
		{"something", false},
	}
	for _, c := range cases {
		s := Spec{Mode: c.mode}
		if got := s.enforce(); got != c.want {
			t.Errorf("Spec{%q}.enforce() = %v, want %v", c.mode, got, c.want)
		}
	}
}

// --- Spec zero value ---

func TestSpecZeroValue(t *testing.T) {
	var s Spec
	if s.enforce() {
		t.Error("zero-value Spec should not enforce")
	}
	if s.Network {
		t.Error("zero-value Spec should not allow network")
	}
	if len(s.WriteRoots) != 0 {
		t.Error("zero-value Spec should have no write roots")
	}
}

// --- Command ---

func TestCommandNonEnforce(t *testing.T) {
	spec := Spec{Mode: "off"}
	cmd, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "bash"}, "ls")
	if wrapped {
		t.Error("non-enforce should not wrap")
	}
	if cmd[0] != "bash" {
		t.Errorf("cmd[0] = %q, want bash", cmd[0])
	}
}

func TestCommandEmptyMode(t *testing.T) {
	spec := Spec{}
	cmd, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "sh"}, "echo hi")
	if wrapped {
		t.Error("empty mode should not wrap")
	}
	if len(cmd) != 3 {
		t.Errorf("cmd length = %d, want 3", len(cmd))
	}
}

func TestCommandPowerShell(t *testing.T) {
	cmd, wrapped := Command(Spec{Mode: "off"}, Shell{Kind: ShellPowerShell, Path: "powershell"}, "Get-ChildItem")
	if wrapped {
		t.Error("non-enforce should not wrap")
	}
	want := []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", psUTF8Prologue + "Get-ChildItem"}
	if len(cmd) != len(want) {
		t.Fatalf("argv = %v, want %v", cmd, want)
	}
	for i := range want {
		if cmd[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, cmd[i], want[i])
		}
	}
}

func TestResolveShellDecisionTable(t *testing.T) {
	onPath := func(names ...string) func(string) (string, error) {
		set := map[string]bool{}
		for _, n := range names {
			set[n] = true
		}
		return func(name string) (string, error) {
			if set[name] {
				return `C:\fake\` + name + ".exe", nil
			}
			return "", exec.ErrNotFound
		}
	}
	gitBash := []string{`C:\fake\Git\bin\bash.exe`}
	always := func(string) bool { return true }
	never := func(string) bool { return false }
	cases := []struct {
		name       string
		goos       string
		lookPath   func(string) (string, error)
		candidates []string
		exists     func(string) bool
		probe      func(string) bool
		wantKind   ShellKind
	}{
		{"bash on PATH wins", "windows", onPath("bash", "powershell"), gitBash, never, always, ShellBash},
		{"bash on PATH but probe fails", "windows", onPath("bash", "powershell"), gitBash, never, never, ShellPowerShell},
		{"no bash, git-bash on disk", "windows", onPath("powershell"), gitBash, always, always, ShellBash},
		{"git-bash on disk but probe fails", "windows", onPath("powershell"), gitBash, always, never, ShellPowerShell},
		{"no bash anywhere, pwsh", "windows", onPath("pwsh", "powershell"), gitBash, never, never, ShellPowerShell},
		{"no bash, only powershell", "windows", onPath("powershell"), gitBash, never, never, ShellPowerShell},
		{"windows, nothing found", "windows", onPath(), nil, never, never, ShellBash},
		{"linux, no bash → no PS fallback", "linux", onPath("powershell"), gitBash, always, always, ShellBash},
	}
	for _, c := range cases {
		got := resolveShell(c.goos, c.lookPath, c.exists, c.candidates, c.probe)
		if got.Kind != c.wantKind {
			t.Errorf("%s: kind = %s, want %s (path=%s)", c.name, got.Kind, c.wantKind, got.Path)
		}
	}
}

func TestSupportsChaining(t *testing.T) {
	cases := []struct {
		sh   Shell
		want bool
	}{
		{Shell{Kind: ShellBash, Path: "bash"}, true},
		{Shell{Kind: ShellPowerShell, Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, false},
		{Shell{Kind: ShellPowerShell, Path: "powershell"}, false},
		{Shell{Kind: ShellPowerShell, Path: `C:\Program Files\PowerShell\7\pwsh.exe`}, true},
		{Shell{Kind: ShellPowerShell, Path: "pwsh"}, true},
	}
	for _, c := range cases {
		if got := c.sh.SupportsChaining(); got != c.want {
			t.Errorf("SupportsChaining(%+v) = %v, want %v", c.sh, got, c.want)
		}
	}
}

func TestShellArgvDefaultsPath(t *testing.T) {
	if got := (Shell{Kind: ShellBash}).argv("ls"); got[0] != "bash" {
		t.Errorf("empty bash path argv[0] = %q, want bash", got[0])
	}
	if got := (Shell{Kind: ShellPowerShell}).argv("ls"); got[0] != "powershell" {
		t.Errorf("empty powershell path argv[0] = %q, want powershell", got[0])
	}
}

// --- Command (platform-specific) ---

func TestCommandNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/tmp"}}
	cmd, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "sh"}, "echo hi")
	if wrapped {
		t.Error("non-darwin should never wrap")
	}
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != "echo hi" {
		t.Errorf("unexpected cmd: %v", cmd)
	}
}

func TestCommandDarwinEnforce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	if !Available() {
		t.Skip("sandbox-exec not available")
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/workspace"}}
	cmd, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "sh"}, "echo hi")
	if !wrapped {
		t.Error("darwin enforce with sandbox-exec should wrap")
	}
	if cmd[0] != "sandbox-exec" {
		t.Errorf("cmd[0] = %q, want sandbox-exec", cmd[0])
	}
	if len(cmd) != 6 {
		t.Errorf("cmd length = %d, want 6", len(cmd))
	}
}

func TestCommandDarwinNonEnforce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	spec := Spec{Mode: "off", WriteRoots: []string{"/workspace"}}
	_, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "sh"}, "echo hi")
	if wrapped {
		t.Error("non-enforce should not wrap even on darwin")
	}
}

// --- Available ---

func TestAvailableNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	if Available() {
		t.Error("non-darwin should report unavailable")
	}
}
