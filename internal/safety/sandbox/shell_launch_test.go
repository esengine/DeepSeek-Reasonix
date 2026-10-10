package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/secrets"
)

func TestWindowsPowerShellCandidatesIncludeStoreAlias(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\PF`)
	t.Setenv("ProgramW6432", "")
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("LOCALAPPDATA", `C:\LAD`)
	t.Setenv("SystemRoot", `C:\WIN`)
	got := windowsPowerShellCandidates()
	want := []string{
		filepath.Join(`C:\PF`, "PowerShell", "7", "pwsh.exe"),
		filepath.Join(`C:\LAD`, "Microsoft", "WindowsApps", "pwsh.exe"),
		filepath.Join(`C:\WIN`, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
}

// A Store pwsh that is found but cannot start must not win auto-detection: the
// next interpreter that does start is selected instead. Only the candidate about
// to win is asked, so a machine that ends on bash never probes PowerShell.
func TestAutoSkipsPowerShellThatDoesNotLaunch(t *testing.T) {
	alias := `C:\fake\WindowsApps\pwsh.exe`
	winPS := []string{alias, `C:\fake\System32\powershell.exe`}
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	var asked []string
	launches := func(p string) bool { asked = append(asked, p); return p != alias && p != `C:\fake\pwsh.exe` }

	h := shellHost{"windows", fakePath("pwsh", "powershell"), yes, nil, winPS, no, no, launches, nil}
	if got := h.auto(nil); got.Kind != ShellPowerShell || got.Path != `C:\fake\System32\powershell.exe` {
		t.Fatalf("auto = %+v, want Windows PowerShell 5.1", got)
	}
	if want := []string{alias, `C:\fake\pwsh.exe`, `C:\fake\System32\powershell.exe`}; !slices.Equal(asked, want) {
		t.Fatalf("asked %q, want %q", asked, want)
	}

	h.launches = yes
	if got := h.auto(nil); got.Path != alias {
		t.Fatalf("auto = %+v, want the Store pwsh when it launches", got)
	}

	asked = nil
	h = shellHost{"windows", fakePath("bash", "pwsh"), yes, nil, winPS, yes, no, launches, nil}
	if got := h.auto(nil); got.Kind != ShellBash || len(asked) != 0 {
		t.Fatalf("auto = %+v, asked %q; want bash with no PowerShell probe", got, asked)
	}
}

func TestPowerShellProbeRunsNothingOfTheUsers(t *testing.T) {
	cmd := powerShellProbeCommand(context.Background(), `C:\x\pwsh.exe`)
	if want := []string{`C:\x\pwsh.exe`, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "exit 0"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %q", cmd.Args)
	}
	if !slices.Equal(cmd.Env, secrets.ProcessEnv()) || cmd.Dir != os.TempDir() {
		t.Fatalf("env/dir not scrubbed: dir=%q", cmd.Dir)
	}
}

// Only an execution alias pays a launch; a regular executable is taken as found.
func TestPowerShellLaunchesProbesOnlyAliases(t *testing.T) {
	probed := 0
	saved := aliasLaunches
	aliasLaunches = &launchProbes{seen: map[string]launchProbe{}, run: func(string) bool { probed++; return false }}
	t.Cleanup(func() { aliasLaunches = saved })

	regular := filepath.Join(t.TempDir(), "pwsh.exe")
	if err := os.WriteFile(regular, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !powerShellLaunches(regular) || probed != 0 {
		t.Fatalf("regular file: launches=%v probes=%d, want true without a probe", powerShellLaunches(regular), probed)
	}
}

func TestLaunchProbesAnswerRepeatedDiscoveryOnce(t *testing.T) {
	runs := 0
	l := &launchProbes{seen: map[string]launchProbe{}, run: func(string) bool { runs++; return true }}
	for range 3 {
		if !l.check(`C:\Users\u\AppData\Local\Microsoft\WindowsApps\pwsh.exe`) {
			t.Fatal("probe answer lost")
		}
	}
	if runs != 1 {
		t.Fatalf("probe ran %d times, want 1", runs)
	}
}

// A missing path is not a launchable Store alias; callers that gate on this
// helper must not keep an interpreter that the host does not have.
func TestPowerShellLaunchesRejectsMissingPath(t *testing.T) {
	if powerShellLaunches(filepath.Join(t.TempDir(), "missing-pwsh.exe")) {
		t.Fatal("missing PowerShell reported as launchable")
	}
}
