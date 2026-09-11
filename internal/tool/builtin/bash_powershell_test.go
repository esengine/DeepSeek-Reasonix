package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func powershellPath(t *testing.T) string {
	t.Helper()
	for _, n := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("no PowerShell on PATH")
	return ""
}

func runPS(t *testing.T, command string) (string, error) {
	t.Helper()
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}}
	args, _ := json.Marshal(map[string]string{"command": command})
	return b.Execute(context.Background(), args)
}

func TestBashPowerShellRunsNativeCommand(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	out, err := runPS(t, "Write-Output reasonix-ok")
	if err != nil {
		t.Fatalf("powershell command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "reasonix-ok") {
		t.Fatalf("output = %q, want it to contain reasonix-ok", out)
	}
}

func TestBashPowerShellSurfacesNonZeroExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	if _, err := runPS(t, "exit 3"); err == nil {
		t.Fatal("non-zero exit should surface as an error")
	}
}

func TestBashPowerShellRejectsChaining(t *testing.T) {
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"}}
	for _, cmd := range []string{"echo a && echo b", "echo a || echo b"} {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		out, err := b.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("%q should be rejected on powershell, got out=%q", cmd, out)
		} else if !strings.Contains(err.Error(), "PowerShell") {
			t.Errorf("%q error should explain PowerShell, got %v", cmd, err)
		}
	}
}

func TestBashPowerShellAllowsQuotedOperator(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("runs a real powershell command")
	}
	// "&&" inside a string literal is data, not chaining — must not be rejected.
	out, err := runPS(t, `Write-Output "a && b"`)
	if err != nil {
		t.Fatalf("quoted && should run: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "a && b") {
		t.Fatalf("output = %q", out)
	}
}

func TestBashPwshAllowsChaining(t *testing.T) {
	// pwsh (PowerShell 7+) parses && — the guard must not block it.
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "pwsh"}}
	args, _ := json.Marshal(map[string]string{"command": "echo a && echo b"})
	_, err := b.Execute(context.Background(), args)
	if err != nil && strings.Contains(err.Error(), "does not parse") {
		t.Errorf("pwsh should not be blocked by the chaining guard: %v", err)
	}
}

func TestBashPowerShellOutputIsUTF8(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	out, err := runPS(t, "Write-Output 'AB-中文-CD'")
	if err != nil {
		t.Fatalf("command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "中文") {
		t.Fatalf("non-ASCII output mojibake — got %q (want it to contain 中文)", out)
	}
}

// TestBashPowerShellExecuteDetailedContract is the Windows CI contract for the
// shell execution metadata path: Chinese workspace path, UTF-8 output, exit
// code preservation (including 0), and PowerShell 5.1 vs pwsh identity.
func TestBashPowerShellExecuteDetailedContract(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only; Linux/macOS CI covers bash path")
	}
	// Windows PowerShell 5.1 is the compatibility-critical path: unlike pwsh,
	// it does not parse &&/|| and commonly runs under a legacy console code page.
	// Require it on native Windows instead of silently selecting pwsh first.
	ps51Path, err := exec.LookPath("powershell")
	if err != nil {
		t.Fatalf("Windows PowerShell 5.1 is required for this contract: %v", err)
	}
	paths := []struct {
		name string
		path string
	}{{name: "powershell-5.1", path: ps51Path}}
	// PowerShell 7 is optional for ordinary Windows installations, but the
	// GitHub Windows runner provides it; exercise it whenever available.
	if pwshPath, lookupErr := exec.LookPath("pwsh"); lookupErr == nil {
		paths = append(paths, struct {
			name string
			path string
		}{name: "pwsh-7", path: pwshPath})
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			assertPowerShellDetailedContract(t, tc.path)
		})
	}
}

func assertPowerShellDetailedContract(t *testing.T, psPath string) {
	t.Helper()
	// Chinese directory name — native Windows CI must keep path + UTF-8 intact.
	work := filepath.Join(t.TempDir(), "中文目录-reasonix")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(work, "标记.txt")
	if err := os.WriteFile(marker, []byte("内容-utf8"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: psPath}, workDir: work}

	// Success: exit 0 must be retained (not omitted) and UTF-8 content preserved.
	argsOK, _ := json.Marshal(map[string]string{
		"command": "Get-Content -LiteralPath .\\标记.txt -Encoding utf8; Write-Output '中文-ok'",
	})
	res, err := b.ExecuteDetailed(context.Background(), argsOK)
	if err != nil {
		t.Fatalf("success path: %v out=%q", err, res.Output)
	}
	if res.Execution == nil {
		t.Fatal("missing execution metadata")
	}
	if res.Execution.State != tool.ShellStateCompleted {
		t.Fatalf("state=%q", res.Execution.State)
	}
	if res.Execution.ExitCode == nil || *res.Execution.ExitCode != 0 {
		t.Fatalf("exitCode=%v want 0", res.Execution.ExitCode)
	}
	if !strings.Contains(res.Output, "内容-utf8") || !strings.Contains(res.Output, "中文-ok") {
		t.Fatalf("UTF-8/Chinese lost in combined output: %q", res.Output)
	}
	if !utf8.ValidString(res.Output) {
		t.Fatal("combined output is not valid UTF-8")
	}
	// Descriptor identity: powershell.exe → 5.1; pwsh → 7+.
	base := strings.ToLower(filepath.Base(psPath))
	base = strings.TrimSuffix(base, ".exe")
	if base == "pwsh" {
		if res.Execution.Shell != tool.ShellNamePwsh || res.Execution.ShellVersion != tool.ShellVersionPS7 {
			t.Fatalf("pwsh identity = %s/%s", res.Execution.Shell, res.Execution.ShellVersion)
		}
		if !res.Execution.SupportsAndAnd {
			t.Fatal("pwsh should support &&")
		}
	} else {
		if res.Execution.Shell != tool.ShellNamePowerShell || res.Execution.ShellVersion != tool.ShellVersionPS51 {
			t.Fatalf("powershell identity = %s/%s", res.Execution.Shell, res.Execution.ShellVersion)
		}
		if res.Execution.SupportsAndAnd {
			t.Fatal("Windows PowerShell 5.1 must not claim && support")
		}
	}

	// Non-zero exit: preserve real code and execution failure phase.
	argsFail, _ := json.Marshal(map[string]string{"command": "exit 17"})
	fail, err := b.ExecuteDetailed(context.Background(), argsFail)
	if err == nil {
		t.Fatal("exit 17 should error")
	}
	if fail.Execution == nil || fail.Execution.ExitCode == nil || *fail.Execution.ExitCode != 17 {
		t.Fatalf("exit metadata = %+v", fail.Execution)
	}
	if fail.Execution.State != tool.ShellStateFailed || fail.Execution.FailurePhase != tool.ShellPhaseExecution {
		t.Fatalf("fail state/phase = %s/%s", fail.Execution.State, fail.Execution.FailurePhase)
	}
}

func TestBashPowerShell51PreflightRejectsAndAndDetailed(t *testing.T) {
	// Runs on every OS: pure preflight, no process launch.
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"}}
	args, _ := json.Marshal(map[string]string{"command": "echo a && echo b"})
	res, err := b.ExecuteDetailed(context.Background(), args)
	if err == nil {
		t.Fatal("expected preflight rejection")
	}
	if res.Execution == nil {
		t.Fatal("missing execution")
	}
	if res.Execution.State != tool.ShellStateNotRun || res.Execution.FailurePhase != tool.ShellPhasePreflight {
		t.Fatalf("state/phase = %s/%s", res.Execution.State, res.Execution.FailurePhase)
	}
	if res.Execution.MutationRisk != tool.ShellMutationNotStarted {
		t.Fatalf("mutationRisk = %q", res.Execution.MutationRisk)
	}
	if res.Execution.ExitCode != nil {
		t.Fatalf("exitCode must be unset for preflight, got %v", *res.Execution.ExitCode)
	}
}

func TestBashDescriptionReflectsShell(t *testing.T) {
	ps := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"}}
	psDesc := ps.Description()
	if !strings.Contains(psDesc, "Windows PowerShell") {
		t.Errorf("powershell description should name Windows PowerShell: %q", psDesc)
	}
	if !strings.Contains(psDesc, "'&&' and '||' are NOT parsed") {
		t.Errorf("powershell description should warn about unsupported chaining: %q", psDesc)
	}
	pwsh := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "pwsh"}}
	pwshDesc := pwsh.Description()
	if !strings.Contains(pwshDesc, "PowerShell 7 (pwsh)") {
		t.Errorf("pwsh description should name PowerShell 7: %q", pwshDesc)
	}
	if !strings.Contains(pwshDesc, "'&&' and '||' are parsed") {
		t.Errorf("pwsh description should allow conditional chaining: %q", pwshDesc)
	}
	if strings.Contains(pwshDesc, "NOT parsed") {
		t.Errorf("pwsh description should not reuse the Windows PowerShell chaining warning: %q", pwshDesc)
	}
	sh := bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}}
	if strings.Contains(sh.Description(), "PowerShell") {
		t.Errorf("bash description should not mention PowerShell: %q", sh.Description())
	}
}
