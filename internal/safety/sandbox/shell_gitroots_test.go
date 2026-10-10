package sandbox

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitInstallRootsAreAppendedAfterEveryExistingCandidate(t *testing.T) {
	existing := []string{filepath.Join(`C:\Program Files\Git`, "bin", "bash.exe")}
	got := appendGitInstallRoots(existing, []string{`D:\tools\Git`})
	want := []string{
		existing[0],
		filepath.Join(`D:\tools\Git`, "bin", "bash.exe"),
		filepath.Join(`D:\tools\Git`, "usr", "bin", "bash.exe"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGitInstallRootsDropDuplicatesAndUnsafeRoots(t *testing.T) {
	dup := filepath.Join(`C:\Git`, "bin", "bash.exe")
	got := appendGitInstallRoots([]string{dup}, []string{
		`c:\git`,             // the install already found, spelled differently
		`\\server\share\Git`, // network share
		`\\?\D:\Git`,         // extended-length path
		`Git`,                // relative
		`D:Git`,              // drive-relative
		``,
		`  `,
		`E:\Apps\Git`,
		`E:\Apps\Git`, // listed twice
	})
	want := []string{
		dup,
		// The same install spelled differently still contributes the candidate it has
		// not yet been seen with.
		filepath.Join(`c:\git`, "usr", "bin", "bash.exe"),
		filepath.Join(`E:\Apps\Git`, "bin", "bash.exe"),
		filepath.Join(`E:\Apps\Git`, "usr", "bin", "bash.exe"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGitInstallRootOKAcceptsOnlyDriveAbsoluteDirectories(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Git`:          true,
		`d:/Git`:          true,
		` C:\Git `:        false, // callers trim first, so the string checked is the string used
		`\\srv\share\Git`: false,
		`\\?\C:\Git`:      false,
		`\\.\C:\Git`:      false,
		`C:Git`:           false,
		`Git`:             false,
		`/usr/local/git`:  false,
		`1:\Git`:          false,
		``:                false,
	} {
		if got := gitInstallRootOK(p); got != want {
			t.Errorf("gitInstallRootOK(%q) = %v, want %v", p, got, want)
		}
	}
}

// A registry value padded with spaces used to pass the check trimmed and then be
// joined untrimmed, yielding a relative candidate such as " C:\Git \bin\bash.exe".
func TestPaddedGitInstallRootYieldsACleanAbsoluteCandidate(t *testing.T) {
	got := appendGitInstallRoots(nil, []string{"  " + `C:\Git\Tools` + " "})
	want := []string{
		filepath.Join(`C:\Git\Tools`, "bin", "bash.exe"),
		filepath.Join(`C:\Git\Tools`, "usr", "bin", "bash.exe"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, p := range got {
		if strings.HasPrefix(p, " ") || strings.Contains(p, " \\") {
			t.Fatalf("candidate %q carries the padding", p)
		}
	}
}

// The point of the registry source: a Git outside the standard roots whose git is
// not on PATH is found and wins over PowerShell.
func TestRegistryRootIsFoundAndChosenOverPowerShell(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cands := appendGitInstallRoots(nil, []string{`D:\Tools\Git`})
	want := cands[0]
	h := shellHost{"windows", fakePath("pwsh"), func(p string) bool { return p == want }, cands, []string{`C:\fake\pwsh.exe`}, yes, no, yes, nil}
	var warn strings.Builder
	sh := h.auto(&warn)
	if sh.Kind != ShellBash || sh.Path != want || sh.Fallback != FallbackNone {
		t.Fatalf("got %+v, want plain bash at %s", sh, want)
	}
	if warn.Len() != 0 {
		t.Fatalf("a found bash must not warn: %q", warn.String())
	}
}

func TestProbeFailureReasonIsATimeoutOnlyWhenTheDeadlineExpired(t *testing.T) {
	if got := probeFailureReason(context.DeadlineExceeded); got != FallbackProbeTimeout {
		t.Fatalf("deadline: got %q", got)
	}
	if got := probeFailureReason(context.Canceled); got != FallbackProbeFailed {
		t.Fatalf("canceled: got %q", got)
	}
	if got := probeFailureReason(nil); got != FallbackProbeFailed {
		t.Fatalf("a command that ran and failed: got %q", got)
	}
}

// What the warning promises must be true: a PowerShell 7 fallback runs && chains, a
// null redirect is rewritten, and the way to silence the warning is named.
func TestFallbackWarningDoesNotOverpromise(t *testing.T) {
	var w strings.Builder
	warnBashFallback(&w, Shell{Kind: ShellPowerShell, Path: `C:\fake\pwsh.exe`, Fallback: FallbackNotFound})
	msg := w.String()
	for _, bad := range []string{"&&", "/dev/null"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("the warning claims %q fails, but PowerShell 7 chains and the redirect is rewritten: %q", bad, msg)
		}
	}
	for _, want := range []string{"head or grep", `prefer="pwsh"`, "bash.exe"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the warning should mention %q: %q", want, msg)
		}
	}
}

// prefer="powershell" would pick Windows PowerShell 5.1 ahead of pwsh, so the advice
// must name the interpreter that is in use, or silencing the warning costs && chains.
func TestFallbackWarningAdviceKeepsTheInterpreterInUse(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\Program Files\PowerShell\7\pwsh.exe`, `prefer="pwsh"`},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `prefer="powershell"`},
	} {
		var w strings.Builder
		warnBashFallback(&w, Shell{Kind: ShellPowerShell, Path: tc.path, Fallback: FallbackNotFound})
		if !strings.Contains(w.String(), tc.want) {
			t.Errorf("for %s want %s in %q", tc.path, tc.want, w.String())
		}
	}
}

// Without a Git Bash that runs, the session falls back to PowerShell and says why:
// the warning is the only place the user hears that bash syntax will now fail.
func TestFallbackToPowerShellIsTypedAndWarned(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	ps := []string{`C:\fake\PowerShell\7\pwsh.exe`}
	gitBash := []string{`C:\fake\Git\bin\bash.exe`}
	cases := []struct {
		name   string
		host   shellHost
		prime  func()
		want   FallbackReason
		inWarn string
	}{
		{"no candidate exists", shellHost{"windows", fakePath(), yes, nil, ps, yes, no, yes, nil}, nil, FallbackNotFound, "no Git Bash was found"},
		{"candidate exists but did not run", shellHost{"windows", fakePath(), yes, gitBash, ps, no, no, yes, nil}, nil, FallbackProbeFailed, "did not run a command"},
		{"candidate timed out", shellHost{"windows", fakePath(), yes, gitBash, ps, no, no, yes, nil}, func() { bashProbeFailures.Store(gitBash[0], FallbackProbeTimeout) }, FallbackProbeTimeout, "did not answer in time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { bashProbeFailures.Delete(gitBash[0]) })
			if tc.prime != nil {
				tc.prime()
			}
			var warn strings.Builder
			sh := tc.host.auto(&warn)
			if sh.Kind != ShellPowerShell || sh.Fallback != tc.want {
				t.Fatalf("got kind=%v fallback=%q, want PowerShell with %q", sh.Kind, sh.Fallback, tc.want)
			}
			if !strings.Contains(warn.String(), tc.inWarn) || !strings.Contains(warn.String(), "using PowerShell") {
				t.Fatalf("warning does not say why: %q", warn.String())
			}
		})
	}
}

func TestBashThatRunsIsNotAFallback(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	h := shellHost{"windows", fakePath(), yes, []string{`C:\fake\Git\bin\bash.exe`}, nil, yes, no, yes, nil}
	sh := h.auto(io.Discard)
	if sh.Kind != ShellBash || sh.Fallback != FallbackNone {
		t.Fatalf("got kind=%v fallback=%q, want plain bash", sh.Kind, sh.Fallback)
	}
}

func TestProbeTimeoutAllowsAColdStart(t *testing.T) {
	if bashProbeTimeout < 10*time.Second {
		t.Fatalf("a bash probe that gives up before 10s drops the session's bash on a slow first launch: %v", bashProbeTimeout)
	}
}
