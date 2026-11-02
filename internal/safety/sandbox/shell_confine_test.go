package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func confineHost(t *testing.T, confine func(string) bool, bashPath string) shellHost {
	t.Helper()
	t.Cleanup(func() { bashConfineRefusals.Delete(bashPath) })
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	probe := func(p string) bool { return confinedAt(p, confine) }
	return shellHost{"windows", fakePath("powershell"), yes, []string{bashPath}, []string{`C:\fake\System32\powershell.exe`}, probe, no, yes, nil}
}

func TestAutoPassesOverABashTheSandboxRefuses(t *testing.T) {
	const bash = `C:\fake\confine-refused\bin\bash.exe`
	h := confineHost(t, func(string) bool { return false }, bash)
	var warn strings.Builder
	sh := h.auto(&warn)
	if sh.Kind != ShellPowerShell || sh.Fallback != FallbackSandboxRefused {
		t.Fatalf("shell = %+v, want PowerShell with the sandbox as the reason", sh)
	}
	for _, want := range []string{"cannot start inside the sandbox", "Turn the sandbox off to use Git Bash", `prefer="powershell"`} {
		if !strings.Contains(warn.String(), want) {
			t.Errorf("warning %q lacks %q", warn.String(), want)
		}
	}
}

func TestAutoKeepsABashTheSandboxAccepts(t *testing.T) {
	const bash = `C:\fake\confine-accepted\bin\bash.exe`
	h := confineHost(t, func(string) bool { return true }, bash)
	sh := h.auto(nil)
	if sh.Kind != ShellBash || sh.Path != bash || sh.Fallback != FallbackNone {
		t.Fatalf("shell = %+v, want the bash kept", sh)
	}
}

func TestAShellThatFailsItsOwnProbeIsNotBlamedOnTheSandbox(t *testing.T) {
	const bash = `C:\fake\confine-broken\bin\bash.exe`
	h := confineHost(t, func(string) bool { return true }, bash)
	h.probe = func(string) bool { return false }
	sh := h.auto(nil)
	if sh.Kind != ShellPowerShell || sh.Fallback != FallbackProbeFailed {
		t.Fatalf("shell = %+v, want the broken bash reported as a failed probe", sh)
	}
}

func TestAPinnedBashTheSandboxRefusesFallsBackToo(t *testing.T) {
	const bash = `C:\fake\confine-pinned\bin\bash.exe`
	h := confineHost(t, func(string) bool { return false }, bash)
	var warn strings.Builder
	sh := resolveOn(h, "bash", bash, &warn)
	if sh.Kind != ShellPowerShell || sh.Fallback != FallbackSandboxRefused {
		t.Fatalf("shell = %+v, want PowerShell because the sandbox refused the pinned bash", sh)
	}
	if !strings.Contains(warn.String(), "not a usable bash") {
		t.Errorf("the pin was not reported as unusable: %q", warn.String())
	}
}

func TestBashConfineIsAbsentUnlessTheModeEnforces(t *testing.T) {
	for _, mode := range []string{"", "off", "warn"} {
		if BashConfine(mode, t.TempDir()) != nil {
			t.Errorf("mode %q asked the shell to start confined", mode)
		}
	}
}

func TestResolveAsksConfineOfEveryBashItProves(t *testing.T) {
	bash := filepath.Join(t.TempDir(), "bash")
	if err := os.WriteFile(bash, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bashConfineRefusals.Delete(bash) })
	look := func(name string) (string, error) {
		if name == "bash" {
			return bash, nil
		}
		return "", exec.ErrNotFound
	}
	asked := map[string]bool{}
	d := ShellDiscovery{Probe: func(string) bool { return true }, LookPath: look, Confine: func(p string) bool { asked[p] = true; return false }}
	if sh := d.Resolve(); sh.Path == bash {
		t.Fatalf("the bash the sandbox refused was chosen: %+v", sh)
	}
	if !asked[bash] {
		t.Fatalf("confine was never asked about %s (asked %v)", bash, asked)
	}
	asked = map[string]bool{}
	d.Confine = nil
	if sh := d.Resolve(); sh.Path != bash {
		t.Fatalf("without confinement the proven bash should win, got %+v", sh)
	}
}
