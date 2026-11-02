//go:build windows

package sandbox

import (
	"strings"
	"testing"
)

// Git for Windows' bash exits at start under the write-restricted token
// ("couldn't create signal pipe", Win32 error 5) while cmd.exe runs: the shell a
// sandboxed session chooses must be one that starts.
func TestBashConfineRefusesGitBashAndAcceptsAShellThatStarts(t *testing.T) {
	h := newWinHarness(t)
	confine := BashConfine("enforce", h.state)
	if confine == nil {
		t.Fatal("an enforcing mode asked for no confinement check")
	}
	if argv, _ := CommandArgs(h.spec, []string{"cmd", "/c", "exit 0"}); !startsConfined(argv) {
		t.Error("cmd.exe starts under the token, but the probe said it did not")
	}
	var gitBash string
	for _, p := range windowsBashCandidates() {
		if fileExists(p) && !isWindowsWSLBash(p) {
			gitBash = p
			break
		}
	}
	if gitBash == "" {
		t.Skip("Git for Windows is not installed")
	}
	if confine(gitBash) {
		t.Errorf("%s was accepted, but a command through it cannot start under the token", gitBash)
	}
	if _, code := h.runArgv(Shell{Kind: ShellBash, Path: gitBash}.argv("echo up")); code == 0 {
		t.Errorf("the check refuses %s but the launcher ran it", gitBash)
	}
	var warn strings.Builder
	picked := ShellDiscovery{Confine: confine, Warn: &warn}.Resolve()
	if picked.Kind != ShellPowerShell || picked.Fallback != FallbackSandboxRefused {
		t.Errorf("a sandboxed session picked %+v, want PowerShell because the sandbox refused Git Bash\n%s", picked, warn.String())
	}
	if free := (ShellDiscovery{}).Resolve(); free.Kind != ShellBash {
		t.Errorf("without a sandbox the session picked %+v, want Git Bash", free)
	}
}
