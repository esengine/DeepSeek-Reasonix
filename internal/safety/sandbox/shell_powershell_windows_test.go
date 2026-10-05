//go:build windows

package sandbox

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/proc"
)

// installedPowerShells returns every real PowerShell on this host, 5.1 first.
func installedPowerShells(t *testing.T) []Shell {
	t.Helper()
	var out []Shell
	seen := map[string]bool{}
	for _, p := range windowsPowerShellCandidates() {
		if fileExists(p) && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, Shell{Kind: ShellPowerShell, Path: p})
		}
	}
	if p, err := exec.LookPath("pwsh"); err == nil && !seen[strings.ToLower(p)] {
		out = append(out, Shell{Kind: ShellPowerShell, Path: p})
	}
	if len(out) == 0 {
		t.Skip("no PowerShell on this host")
	}
	return out
}

func isWindowsPowerShell(sh Shell) bool {
	return strings.EqualFold(pathBase(sh.Path), "powershell.exe")
}

// runPowerShell runs script under sh in dir. setup, when set, runs first and then
// hands script to Invoke-Expression, so it can put the session into a state the
// script has to survive: no console, or ConstrainedLanguage.
func runPowerShell(t *testing.T, sh Shell, dir, setup, script string) (stdout, stderr string) {
	t.Helper()
	argv := []string{sh.Path, "-NoProfile", "-NonInteractive", "-Command", script}
	env := os.Environ()
	if setup != "" {
		argv[len(argv)-1] = setup + "; Invoke-Expression $env:RX_TEST_SCRIPT"
		env = append(env, "RX_TEST_SCRIPT="+script)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	proc.HideWindow(cmd)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\nstderr: %s", sh.Path, err, se.String())
	}
	return so.String(), se.String()
}

const (
	freeConsole = `Add-Type -Name K -Namespace RxTest -MemberDefinition '[DllImport("kernel32.dll")] public static extern bool FreeConsole();'; $null = [RxTest.K]::FreeConsole()`
	constrained = `$ExecutionContext.SessionState.LanguageMode = 'ConstrainedLanguage'`
)

func assertUTF8File(t *testing.T, path, want string, bom bool) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := []byte(want)
	if bom {
		wantBytes = append([]byte{0xEF, 0xBB, 0xBF}, wantBytes...)
	}
	if !bytes.Equal(got, wantBytes) {
		t.Fatalf("%s = % x, want % x", filepath.Base(path), got, wantBytes)
	}
}

func TestPowerShellToolRedirectWritesUTF8(t *testing.T) {
	for _, sh := range installedPowerShells(t) {
		t.Run(filepath.Base(sh.Path), func(t *testing.T) {
			dir := t.TempDir()
			argv := sh.argv(`'中文' > f.txt; Get-Content f.txt`)
			stdout, stderr := runPowerShell(t, sh, dir, "", argv[len(argv)-1])
			if stderr != "" || stdout != "中文\r\n" {
				t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
			}
			// Windows PowerShell's "utf8" always carries a BOM; 7+ writes none.
			assertUTF8File(t, filepath.Join(dir, "f.txt"), "中文\r\n", isWindowsPowerShell(sh))
		})
	}
}

func TestPowerShellPrologueSurvivesRestrictedSessions(t *testing.T) {
	for _, sh := range installedPowerShells(t) {
		for name, setup := range map[string]string{"no-console": freeConsole, "constrained-language": constrained} {
			t.Run(filepath.Base(sh.Path)+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				argv := sh.argv(`'中文' > f.txt; 'done'`)
				stdout, stderr := runPowerShell(t, sh, dir, setup, argv[len(argv)-1])
				if stderr != "" {
					t.Fatalf("prologue wrote errors: %q", stderr)
				}
				if !strings.HasSuffix(stdout, "done\r\n") {
					t.Fatalf("command did not run after the prologue: %q", stdout)
				}
				assertUTF8File(t, filepath.Join(dir, "f.txt"), "中文\r\n", isWindowsPowerShell(sh))
			})
		}
	}
}

func TestPowerShellPipesUTF8WithoutBOMToNativeCommands(t *testing.T) {
	for _, sh := range installedPowerShells(t) {
		t.Run(filepath.Base(sh.Path), func(t *testing.T) {
			dir := t.TempDir()
			// cmd's own redirect stores findstr's stdout untouched, so the file
			// holds exactly the bytes PowerShell sent down the pipe.
			argv := sh.argv(`'中文' | cmd.exe /d /c 'findstr . > p.txt'`)
			_, stderr := runPowerShell(t, sh, dir, "", argv[len(argv)-1])
			if stderr != "" {
				t.Fatalf("stderr=%q", stderr)
			}
			got, err := os.ReadFile(filepath.Join(dir, "p.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if want := []byte("中文\r\n"); !bytes.Equal(got, want) {
				t.Fatalf("piped bytes = % x, want % x", got, want)
			}
		})
	}
}

// Hooks run user scripts: they get UTF-8 output capture but keep PowerShell's own
// file-writing defaults.
func TestHookPrologueLeavesFileDefaultsAlone(t *testing.T) {
	for _, sh := range installedPowerShells(t) {
		t.Run(filepath.Base(sh.Path), func(t *testing.T) {
			stdout, stderr := runPowerShell(t, sh, t.TempDir(), "", PowerShellUTF8Script(`'[' + $PSDefaultParameterValues['Out-File:Encoding'] + ']'; '中文'`))
			if stderr != "" || stdout != "[]\r\n中文\r\n" {
				t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

// Execution aliases are what the Store installs pwsh as; they must be the paths
// that pay a launch probe.
func TestPowerShellLaunchesProbesRealExecutionAlias(t *testing.T) {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps")
	matches, _ := filepath.Glob(filepath.Join(dir, "*.exe"))
	var alias string
	for _, m := range matches {
		if fi, err := os.Lstat(m); err == nil && fi.Mode()&os.ModeIrregular != 0 {
			alias = m
			break
		}
	}
	if alias == "" {
		t.Skip("no execution alias on this host")
	}
	probed := 0
	saved := aliasLaunches
	aliasLaunches = &launchProbes{seen: map[string]launchProbe{}, run: func(string) bool { probed++; return false }}
	t.Cleanup(func() { aliasLaunches = saved })
	if powerShellLaunches(alias) || probed != 1 {
		t.Fatalf("%s: launches=%v probes=%d, want one failed probe", alias, powerShellLaunches(alias), probed)
	}
}
