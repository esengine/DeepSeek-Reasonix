//go:build windows

package winsandbox

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	waitMS := uint32((15 * time.Second).Milliseconds())
	windowsSandboxWaitMilliseconds = waitMS
	os.Setenv("WINDOWS_SANDBOX_WAIT_MS", strconv.FormatUint(uint64(waitMS), 10))
	os.Exit(m.Run())
}

func TestWindowsAppContainerNameSeparatesForbidReadPolicies(t *testing.T) {
	base := Spec{WritableRoots: []string{`C:\work`}, Network: true}
	baseName := windowsAppContainerName(base)
	forbidName := windowsAppContainerName(Spec{WritableRoots: []string{`C:\work`}, ForbidReadRoots: []string{`C:\work\secret`}, Network: true})
	if baseName == forbidName {
		t.Fatal("different forbid_read roots must not share an AppContainer profile")
	}
	for _, name := range []string{baseName, forbidName} {
		if !strings.HasPrefix(name, "WinSandbox.") || len(name) > 64 {
			t.Fatalf("unexpected AppContainer profile name: %q", name)
		}
	}
}

func TestWindowsAppContainerNetworkCapabilities(t *testing.T) {
	withNetwork, err := prepareAppContainer(Spec{WritableRoots: []string{`C:\work`}, Network: true})
	if err != nil {
		t.Fatalf("prepare AppContainer with network: %v", err)
	}
	defer withNetwork.close()
	if len(withNetwork.capabilities) == 0 {
		t.Fatal("network-enabled AppContainer should include network capabilities")
	}

	withoutNetwork, err := prepareAppContainer(Spec{WritableRoots: []string{`C:\work`}, Network: false})
	if err != nil {
		t.Fatalf("prepare AppContainer without network: %v", err)
	}
	defer withoutNetwork.close()
	if len(withoutNetwork.capabilities) != 0 {
		t.Fatalf("network-disabled AppContainer capabilities = %d, want 0", len(withoutNetwork.capabilities))
	}
}

func TestWindowsCleanupPathSecurityRemovesACEsBeforeRestore(t *testing.T) {
	var calls []string
	cleanup := cleanupPathSecurity(
		func() { calls = append(calls, "restore") },
		func() { calls = append(calls, "remove") },
		func() { calls = append(calls, "after") },
	)
	cleanup()
	if got := strings.Join(calls, ","); got != "remove,restore,after" {
		t.Fatalf("cleanup order = %s, want remove,restore,after", got)
	}
}

func TestWindowsUniqueNonZeroHandles(t *testing.T) {
	got := uniqueNonZeroHandles([]windows.Handle{0, 10, 10, 0, 11, 10})
	want := []windows.Handle{10, 11}
	if len(got) != len(want) {
		t.Fatalf("handles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("handles = %v, want %v", got, want)
		}
	}
}

func TestWindowsSandboxProcessCreationFlagsHideConsole(t *testing.T) {
	flags := windowsSandboxProcessCreationFlags()
	for _, want := range []uint32{
		windows.CREATE_UNICODE_ENVIRONMENT,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		windows.CREATE_SUSPENDED,
		windows.CREATE_NO_WINDOW,
	} {
		if flags&want == 0 {
			t.Fatalf("process creation flags %#x missing %#x", flags, want)
		}
	}
}

func TestWindowsRestrictedProcessCreationFlagsInheritConsole(t *testing.T) {
	flags := windowsRestrictedProcessCreationFlags()
	for _, want := range []uint32{
		windows.CREATE_UNICODE_ENVIRONMENT,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		windows.CREATE_SUSPENDED,
	} {
		if flags&want == 0 {
			t.Fatalf("restricted process flags %#x missing %#x", flags, want)
		}
	}
	if flags&windows.CREATE_NO_WINDOW != 0 {
		t.Fatalf("restricted process flags %#x must omit CREATE_NO_WINDOW", flags)
	}
}

func TestWindowsSandboxStartupInfoHidesWindowAndKeepsStdHandles(t *testing.T) {
	handles := [3]windows.Handle{11, 12, 13}
	si := windowsSandboxStartupInfo(handles, nil)
	if si.Cb == 0 {
		t.Fatal("startup info size was not initialized")
	}
	if si.Flags&windows.STARTF_USESTDHANDLES == 0 {
		t.Fatalf("startup flags %#x missing STARTF_USESTDHANDLES", si.Flags)
	}
	if si.Flags&windows.STARTF_USESHOWWINDOW == 0 {
		t.Fatalf("startup flags %#x missing STARTF_USESHOWWINDOW", si.Flags)
	}
	if si.ShowWindow != windows.SW_HIDE {
		t.Fatalf("ShowWindow = %d, want SW_HIDE", si.ShowWindow)
	}
	if si.StdInput != handles[0] || si.StdOutput != handles[1] || si.StdErr != handles[2] {
		t.Fatalf("std handles = (%v,%v,%v), want %v", si.StdInput, si.StdOutput, si.StdErr, handles)
	}
}

func TestWindowsSandboxSystemCommandsAreHidden(t *testing.T) {
	ctx := t.Context()
	for _, cmd := range []*exec.Cmd{
		hiddenWindowsSystemCommandContext(ctx, "icacls.exe", `C:\work`, "/C"),
		hiddenWindowsSystemCommand("taskkill.exe", "/?"),
	} {
		if cmd.SysProcAttr == nil {
			t.Fatal("system command SysProcAttr is nil")
		}
		if !cmd.SysProcAttr.HideWindow {
			t.Fatal("system command did not set HideWindow")
		}
		if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
			t.Fatalf("system command creation flags %#x missing CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
		}
	}
}

func TestWindowsSandboxAvailableOnCI(t *testing.T) {
	if os.Getenv("CI") == "" {
		t.Skip("only require AppContainer sandbox availability on CI")
	}
	if !Available() {
		t.Fatal("windows sandbox APIs unavailable on CI")
	}
}

func TestWindowsExecutableGrantDirResolvesPathTools(t *testing.T) {
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "windows-sandbox-path-tool.exe")
	if err := os.WriteFile(toolPath, []byte("not really an exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := windowsExecutableGrantDir("windows-sandbox-path-tool.exe"); !sameWindowsPath(got, dir) {
		t.Fatalf("grant dir = %q, want %q", got, dir)
	}
}

func TestWindowsExecutableGrantRootsIncludeGitInstallRoot(t *testing.T) {
	installRoot := filepath.Join(t.TempDir(), "Git")
	bin := filepath.Join(installRoot, "usr", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	bashPath := filepath.Join(bin, "bash.exe")
	if err := os.WriteFile(bashPath, []byte("not really an exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := windowsExecutableGrantRoots(bashPath)
	if len(got) != 2 {
		t.Fatalf("grant roots = %v, want executable dir and Git install root", got)
	}
	if !sameWindowsPath(got[0], bin) || !sameWindowsPath(got[1], installRoot) {
		t.Fatalf("grant roots = %v, want [%s %s]", got, bin, installRoot)
	}
}

func TestWindowsWritableRootsIncludeCommandTempWithoutGlobalTemp(t *testing.T) {
	workspace := t.TempDir()
	commandTemp := t.TempDir()
	got := windowsWritableRoots(Spec{WritableRoots: []string{workspace}}, commandTemp)
	if len(got) != 2 {
		t.Fatalf("writable roots = %v, want workspace and command temp only", got)
	}
	if !sameWindowsPath(got[0], workspace) || !sameWindowsPath(got[1], commandTemp) {
		t.Fatalf("writable roots = %v, want [%s %s]", got, workspace, commandTemp)
	}
	if globalTemp := os.TempDir(); sameWindowsPath(globalTemp, workspace) || sameWindowsPath(globalTemp, commandTemp) {
		t.Skip("test temp dirs are the global temp root")
	}
	for _, root := range got {
		if sameWindowsPath(root, os.TempDir()) {
			t.Fatalf("global temp root should not be auto-granted: %v", got)
		}
	}
}

func TestWindowsSandboxEnvRedirectsTemp(t *testing.T) {
	env := setWindowsEnv([]string{"Path=C:\\Tools", "temp=C:\\old-temp", "TMP=C:\\old-tmp"}, map[string]string{
		"TEMP":   `C:\sandbox-temp`,
		"TMP":    `C:\sandbox-temp`,
		"TMPDIR": `C:\sandbox-temp`,
	})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, want := range []string{"\ntemp=C:\\sandbox-temp\n", "\nTMP=C:\\sandbox-temp\n", "\nTMPDIR=C:\\sandbox-temp\n"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("env %q missing %q", joined, want)
		}
	}
}

func TestWindowsSandboxAllowsWorkspaceWriteAndDeniesOutside(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	outside := t.TempDir()
	insideFile := filepath.Join(workspace, "inside.txt")
	existingFile := filepath.Join(workspace, "existing.txt")
	nestedDir := filepath.Join(workspace, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedExistingFile := filepath.Join(nestedDir, "existing.txt")
	if err := os.WriteFile(existingFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedExistingFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "outside.txt")
	t.Chdir(workspace)
	labelBefore := pathLabelSDDLForTest(t, workspace)

	script := "$ErrorActionPreference='Stop'; " +
		psSandboxDiagnostics(workspace) +
		psTrySetContent(insideFile, "ok") +
		psTrySetContent(existingFile, "updated") +
		psTrySetContent(nestedExistingFile, "nested") +
		"if ((Split-Path -Leaf $env:TEMP) -notlike 'windows-sandbox-test-*') { exit 8 }; " +
		"try { Set-Content -LiteralPath (Join-Path $env:TEMP 'sandbox-temp.txt') -Value temp } catch { Write-Host $_; __winsandbox_dump_diag; exit 1 }; " +
		"try { Set-Content -LiteralPath " + psQuote(outsideFile) + " -Value nope; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("sandbox exit code = %d, want 0", result.ExitCode)
	}
	if got, err := os.ReadFile(insideFile); err != nil || !strings.Contains(string(got), "ok") {
		t.Fatalf("inside write missing: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(existingFile); err != nil || !strings.Contains(string(got), "updated") {
		t.Fatalf("existing file write missing: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(nestedExistingFile); err != nil || !strings.Contains(string(got), "nested") {
		t.Fatalf("nested existing file write missing: %q err=%v", got, err)
	}
	if _, err := os.Stat(outsideFile); err == nil {
		t.Fatalf("outside write unexpectedly succeeded: %s", outsideFile)
	}
	if labelAfter := pathLabelSDDLForTest(t, workspace); labelAfter != labelBefore {
		t.Fatalf("WRITE_RESTRICTED launch changed workspace integrity label:\nbefore: %s\nafter:  %s", labelBefore, labelAfter)
	}
}

func TestWindowsWriteRestrictedReadOnlyAllowsReadsAndDeniesWrites(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	readableFile := filepath.Join(workspace, "readable.txt")
	if err := os.WriteFile(readableFile, []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}
	writtenFile := filepath.Join(workspace, "written.txt")

	script := "$ErrorActionPreference='Stop'; " +
		"$value = Get-Content -Raw -LiteralPath " + psQuote(readableFile) + "; " +
		"if ($value.Trim() -ne 'visible') { exit 8 }; " +
		"try { Set-Content -LiteralPath " + psQuote(writtenFile) + " -Value nope; exit 9 } catch {}; " +
		"try { Set-Content -LiteralPath (Join-Path $env:TEMP 'read-only-temp.txt') -Value nope; exit 10 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, ReadOnly: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("read-only sandbox exit code = %d, want 0", result.ExitCode)
	}
	if _, err := os.Stat(writtenFile); err == nil {
		t.Fatalf("read-only sandbox unexpectedly wrote %s", writtenFile)
	}
}

func TestWindowsCapabilityTempIsIsolatedPerSession(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	tempA := t.TempDir()
	tempB := t.TempDir()
	insideA := filepath.Join(tempA, "inside-a.txt")
	insideB := filepath.Join(tempB, "inside-b.txt")
	// Materialize B's standing temp ACE first. A must still be unable to write B
	// because its token never carries B's capability SID.
	seedB := "Set-Content -LiteralPath " + psQuote(insideB) + " -Value seeded"
	seedResult, err := Run(Spec{WritableRoots: []string{workspace}, TempDir: tempB, Network: true, Writable: true}, append(sh, seedB), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil || seedResult.ExitCode != 0 {
		t.Fatalf("seed sibling session temp: code=%d err=%v", seedResult.ExitCode, err)
	}
	script := "$ErrorActionPreference='Stop'; " +
		"if (-not [IO.Path]::GetFullPath($env:TEMP).Equals([IO.Path]::GetFullPath(" + psQuote(tempA) + "), [StringComparison]::OrdinalIgnoreCase)) { exit 8 }; " +
		"Set-Content -LiteralPath " + psQuote(insideA) + " -Value ok; " +
		"try { Set-Content -LiteralPath " + psQuote(insideB) + " -Value nope; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, TempDir: tempA, Network: true, Writable: true}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("temp isolation exit code = %d, want 0", result.ExitCode)
	}
	if _, err := os.Stat(insideA); err != nil {
		t.Fatalf("session temp write missing: %v", err)
	}
	if got, err := os.ReadFile(insideB); err != nil || !strings.Contains(string(got), "seeded") {
		t.Fatalf("sibling session temp was modified: %q err=%v", got, err)
	}
}

func TestWindowsStandingCapabilitiesDoNotSerializeWholeCommands(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	tempRoot := t.TempDir()
	markers := []string{filepath.Join(workspace, "started-a"), filepath.Join(workspace, "started-b")}
	type runResult struct {
		code int
		err  error
	}
	results := make(chan runResult, 2)
	for i := range 2 {
		own := markers[i]
		peer := markers[1-i]
		script := "$ErrorActionPreference='Stop'; Set-Content -LiteralPath " + psQuote(own) + " -Value started; " +
			"for ($i=0; $i -lt 100; $i++) { if (Test-Path -LiteralPath " + psQuote(peer) + ") { exit 0 }; Start-Sleep -Milliseconds 50 }; exit 9"
		argv := append(append([]string(nil), sh...), script)
		go func(argv []string) {
			result, err := Run(
				Spec{WritableRoots: []string{workspace}, TempDir: tempRoot, Network: true, Writable: true},
				argv,
				RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Dir: workspace},
			)
			results <- runResult{code: result.ExitCode, err: err}
		}(argv)
	}
	for range 2 {
		result := <-results
		if result.err != nil || result.code != 0 {
			t.Fatalf("concurrent restricted command: code=%d err=%v", result.code, result.err)
		}
	}
}

func TestWindowsStandingCapabilityACEIsNotAuthorization(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	extra := t.TempDir()
	target := filepath.Join(extra, "extra.txt")
	seed := "Set-Content -LiteralPath " + psQuote(target) + " -Value authorized"
	seedResult, err := Run(Spec{WritableRoots: []string{workspace, extra}, Network: true, Writable: true}, append(sh, seed), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil || seedResult.ExitCode != 0 {
		t.Fatalf("seed extra capability: code=%d err=%v", seedResult.ExitCode, err)
	}

	probe := "try { Set-Content -LiteralPath " + psQuote(target) + " -Value leaked; exit 9 } catch { exit 0 }"
	probeResult, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: true}, append(sh, probe), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("probe without extra capability: %v", err)
	}
	if probeResult.ExitCode != 0 {
		t.Fatalf("standing capability ACE expanded later token authority, exit=%d", probeResult.ExitCode)
	}
	if got, err := os.ReadFile(target); err != nil || !strings.Contains(string(got), "authorized") {
		t.Fatalf("extra target changed without token capability: %q err=%v", got, err)
	}
}

func TestWindowsRestrictedRuntimeCompatibility(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	workspace := t.TempDir()
	outside := t.TempDir()
	tempRoot := t.TempDir()
	insideFile := filepath.Join(workspace, "runtime-inside.txt")
	outsideFile := filepath.Join(outside, "runtime-outside.txt")

	tests := map[string]func(string, string) []string{
		"powershell-5": func(inside, outside string) []string {
			path, err := exec.LookPath("powershell")
			if err != nil {
				return nil
			}
			script := "$ErrorActionPreference='Stop'; Set-Content -LiteralPath " + psQuote(inside) + " -Value ok; try { Set-Content -LiteralPath " + psQuote(outside) + " -Value nope; exit 9 } catch { exit 0 }"
			return []string{path, "-NoProfile", "-NonInteractive", "-Command", script}
		},
		"powershell-7": func(inside, outside string) []string {
			path, err := exec.LookPath("pwsh")
			if err != nil {
				return nil
			}
			script := "$ErrorActionPreference='Stop'; Set-Content -LiteralPath " + psQuote(inside) + " -Value ok; try { Set-Content -LiteralPath " + psQuote(outside) + " -Value nope; exit 9 } catch { exit 0 }"
			return []string{path, "-NoProfile", "-NonInteractive", "-Command", script}
		},
		"node-inline": func(inside, outside string) []string {
			path, err := exec.LookPath("node")
			if err != nil {
				return nil
			}
			script := `const fs=require('fs');fs.writeFileSync(process.argv[1],'ok');try{fs.writeFileSync(process.argv[2],'nope');process.exit(9)}catch{process.exit(0)}`
			return []string{path, "-e", script, inside, outside}
		},
		"python-inline": func(inside, outside string) []string {
			var path string
			for _, name := range []string{"python3", "python"} {
				if candidate, err := exec.LookPath(name); err == nil {
					path = candidate
					break
				}
			}
			if path == "" {
				return nil
			}
			script := "from pathlib import Path\nimport sys\nPath(sys.argv[1]).write_text('ok')\ntry:\n Path(sys.argv[2]).write_text('nope')\n raise SystemExit(9)\nexcept PermissionError:\n raise SystemExit(0)"
			return []string{path, "-c", script, inside, outside}
		},
	}

	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			argv := command(insideFile, outsideFile)
			if argv == nil {
				t.Skip(name + " is unavailable")
			}
			_ = os.Remove(insideFile)
			_ = os.Remove(outsideFile)
			result, err := Run(
				Spec{WritableRoots: []string{workspace}, TempDir: tempRoot, Network: true, Writable: true},
				argv,
				RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Dir: workspace},
			)
			if err != nil || result.ExitCode != 0 {
				t.Fatalf("runtime launch: code=%d err=%v", result.ExitCode, err)
			}
			if _, err := os.Stat(insideFile); err != nil {
				t.Fatalf("workspace write missing: %v", err)
			}
			if _, err := os.Stat(outsideFile); err == nil {
				t.Fatalf("outside write unexpectedly succeeded: %s", outsideFile)
			}
		})
	}
}

func TestWindowsCapabilityRejectsProtectedRootOverlap(t *testing.T) {
	workspace := t.TempDir()
	tempRoot := t.TempDir()
	protected := filepath.Join(workspace, ".reasonix-state")
	if err := os.Mkdir(protected, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := prepareRestrictedCapabilities(Spec{
		WritableRoots:       []string{workspace},
		ProtectedWriteRoots: []string{protected},
		Network:             true,
		Writable:            true,
	}, tempRoot, nil, "test")
	if err == nil || !strings.Contains(err.Error(), "contains protected state root") {
		t.Fatalf("overlap error = %v, want protected state rejection", err)
	}
}

func TestWindowsCapabilityAllowsExplicitWorkspaceBelowProtectedStateRoot(t *testing.T) {
	protected := t.TempDir()
	workspace := filepath.Join(protected, "global-workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	tempRoot := t.TempDir()
	capabilities, err := prepareRestrictedCapabilities(Spec{
		WritableRoots:       []string{workspace},
		ProtectedWriteRoots: []string{protected},
		Network:             true,
		Writable:            true,
	}, tempRoot, nil, "test")
	if err != nil {
		t.Fatalf("explicit global workspace below state root was rejected: %v", err)
	}
	if len(capabilities) != 2 {
		t.Fatalf("capability count = %d, want workspace and temp", len(capabilities))
	}
}

func TestWindowsCapabilityRejectsArbitraryProtectedStateDescendant(t *testing.T) {
	protected := t.TempDir()
	workspace := filepath.Join(protected, "sessions")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := prepareRestrictedCapabilities(Spec{
		WritableRoots:       []string{workspace},
		ProtectedWriteRoots: []string{protected},
		Network:             true,
		Writable:            true,
	}, t.TempDir(), nil, "test")
	if err == nil || !strings.Contains(err.Error(), "inside protected state root") {
		t.Fatalf("protected descendant error = %v", err)
	}
}

func TestWindowsCapabilityRecordsExactWorkspaceGrantInProtectedState(t *testing.T) {
	root := t.TempDir()
	protected := filepath.Join(root, "state")
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{protected, workspace} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := prepareRestrictedCapabilities(Spec{
		WritableRoots:       []string{workspace},
		ProtectedWriteRoots: []string{protected},
		Network:             true,
		Writable:            true,
	}, t.TempDir(), nil, "test")
	if err != nil {
		t.Fatalf("prepare capabilities: %v", err)
	}
	recordDir := filepath.Join(protected, "windows-sandbox-capabilities-v1")
	entries, err := os.ReadDir(recordDir)
	if err != nil {
		t.Fatalf("read capability records: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("record count = %d, want one workspace record", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(recordDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var record capabilityRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode capability record: %v", err)
	}
	if record.Version != capabilityRecordVersion || record.Status != "active" || record.Purpose != capabilityWorkspace {
		t.Fatalf("capability record = %+v", record)
	}
	if wantPath, pathErr := canonicalWindowsDirectory(workspace); pathErr != nil || !strings.EqualFold(record.CanonicalPath, wantPath) || record.SID == "" || record.AccessMask != uint32(capabilityWriteGrantMask) {
		t.Fatalf("capability record identity = %+v (canonicalize error: %v)", record, pathErr)
	}
	wantIdentity, err := windowsDirectoryIdentity(record.CanonicalPath)
	if err != nil {
		t.Fatalf("read recorded object identity: %v", err)
	}
	if record.VolumeSerial != wantIdentity.volumeSerial || record.FileIndexHigh != wantIdentity.indexHigh || record.FileIndexLow != wantIdentity.indexLow || record.OwnerPID == 0 {
		t.Fatalf("capability record object identity = %+v", record)
	}
}

func TestWindowsCapabilityRecordDirectoryCannotRedirectIntoWorkspace(t *testing.T) {
	root := t.TempDir()
	protected := filepath.Join(root, "state")
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{protected, workspace} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	recordPath := filepath.Join(protected, "windows-sandbox-capabilities-v1")
	if err := os.Symlink(workspace, recordPath); err != nil {
		t.Skipf("directory symlink unavailable for test user: %v", err)
	}
	_, err := newCapabilityRecordStore([]string{protected}, []string{workspace})
	if err == nil || (!strings.Contains(err.Error(), "escapes protected state root") && !strings.Contains(err.Error(), "overlaps writable root")) {
		t.Fatalf("redirected record directory error = %v", err)
	}
}

func TestNormalizeWindowsFinalPath(t *testing.T) {
	tests := map[string]string{
		`\\?\C:\work\repo`:          `C:\work\repo`,
		`\\?\UNC\server\share\repo`: `\\server\share\repo`,
		`C:\work\repo`:              `C:\work\repo`,
	}
	for input, want := range tests {
		if got := normalizeWindowsFinalPath(input); got != want {
			t.Errorf("normalizeWindowsFinalPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalWindowsDirectoryResolvesSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable for test user: %v", err)
	}
	got, err := canonicalWindowsDirectory(link)
	if err != nil {
		t.Fatalf("canonicalize directory symlink: %v", err)
	}
	want, err := canonicalWindowsDirectory(target)
	if err != nil {
		t.Fatalf("canonicalize target: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("canonical symlink path = %q, want %q", got, want)
	}
}

func TestWindowsSandboxDeniesForbidRead(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	secretDir := filepath.Join(workspace, "secret")
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(secretDir, "token.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)

	script := "$ErrorActionPreference='Stop'; " +
		"try { Get-Content -LiteralPath " + psQuote(secretFile) + "; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, ForbidReadRoots: []string{secretDir}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("forbid_read was not enforced, exit code = %d", result.ExitCode)
	}
}

func TestWindowsSandboxDeniesForbidReadInReadOnlyAppContainer(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	secretDir := filepath.Join(workspace, "secret")
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(secretDir, "token.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := "$ErrorActionPreference='Stop'; " +
		"try { Get-Content -LiteralPath " + psQuote(secretFile) + "; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, ForbidReadRoots: []string{secretDir}, Network: true, Writable: false, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("forbid_read was not enforced in read-only AppContainer, exit code = %d", result.ExitCode)
	}
}

func TestWindowsSandboxStdioEnvDirAndExitCode(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace with spaces")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	stdin := tempFileWithContent(t, "stdin.txt", "hello from stdin\n")
	stdout := tempFileWithContent(t, "stdout.txt", "")
	stderr := tempFileWithContent(t, "stderr.txt", "")
	defer stdin.Close()
	defer stdout.Close()
	defer stderr.Close()
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	cwdMarker := filepath.Join(workspace, "cwd-marker.txt")

	script := "$inputText = [Console]::In.ReadToEnd(); " +
		"Write-Output ('OUT:' + $inputText.Trim()); " +
		"[Console]::Error.WriteLine('ERR:' + $env:WINDOWS_SANDBOX_TEST_FLAG); " +
		"try { Set-Content -LiteralPath 'cwd-marker.txt' -Value cwd } catch { exit 7 }; " +
		"if ((Split-Path -Leaf $env:TEMP) -notlike 'windows-sandbox-test-*') { exit 8 }; " +
		"exit 23"
	result, err := Run(
		Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"},
		append(sh, script),
		RunOptions{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
			Env:    append(os.Environ(), "WINDOWS_SANDBOX_TEST_FLAG=flag-from-env"),
			Dir:    workspace,
		},
	)
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 23 {
		t.Fatalf("exit code = %d, want 23", result.ExitCode)
	}
	if got := readWholeFile(t, stdout.Name()); !strings.Contains(got, "OUT:hello from stdin") {
		t.Fatalf("stdout = %q, want stdin echo", got)
	}
	if got := readWholeFile(t, stderr.Name()); !strings.Contains(got, "ERR:flag-from-env") {
		t.Fatalf("stderr = %q, want env echo", got)
	}
	if got, err := os.ReadFile(cwdMarker); err != nil || !strings.Contains(string(got), "cwd") {
		t.Fatalf("cwd marker missing: %q err=%v", got, err)
	}
}

func TestWindowsSandboxNetworkDisabledBlocksLoopbackConnect(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	defer listener.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	script := "$client = [Net.Sockets.TcpClient]::new(); " +
		"$async = $client.BeginConnect('127.0.0.1', " + port + ", $null, $null); " +
		"if ($async.AsyncWaitHandle.WaitOne(1500)) { " +
		"  try { $client.EndConnect($async); $client.Close(); exit 9 } catch { exit 0 } " +
		"} else { $client.Close(); exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: false, Writable: false, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("network-disabled sandbox connected to loopback, exit code = %d", result.ExitCode)
	}
	select {
	case <-accepted:
		t.Fatal("network-disabled sandbox reached the loopback listener")
	default:
	}
}

func TestWindowsSandboxKillsChildProcessTreeOnReturn(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	marker := filepath.Join(workspace, "child-marker.txt")
	childCommand := "Start-Sleep -Seconds 5; Set-Content -LiteralPath " + psQuote(marker) + " -Value alive"
	script := "$exe = (Get-Process -Id $PID).Path; " +
		"$child = Start-Process -FilePath $exe -ArgumentList @('-NoProfile','-NonInteractive','-Command'," + psQuote(childCommand) + ") -PassThru; " +
		"if (-not $child.Id) { exit 8 }; " +
		"exit 0"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("sandbox exit code = %d, want 0", result.ExitCode)
	}
	time.Sleep(7 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("sandbox job object did not kill child process; marker exists: %s", marker)
	}
}

func TestWindowsSandboxTimeoutTerminatesCommand(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	t.Setenv("WINDOWS_SANDBOX_WAIT_MS", "1000")
	workspace := t.TempDir()
	start := time.Now()
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, "Start-Sleep -Seconds 5; exit 9"), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err == nil {
		t.Fatalf("timed-out sandbox should fail, code=%d", result.ExitCode)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
}

func TestWindowsSandboxCleansTouchedSecurityDescriptors(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	secretDir := filepath.Join(workspace, "secret")
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(secretDir, "token.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)

	script := "$ErrorActionPreference='Stop'; " +
		psSandboxDiagnostics(workspace) +
		psTrySetContent(filepath.Join(workspace, "inside.txt"), "ok") +
		"try { Get-Content -LiteralPath " + psQuote(secretFile) + "; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, ForbidReadRoots: []string{secretDir}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("sandbox run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("sandbox exit code = %d, want 0", result.ExitCode)
	}
	assertNoWindowsSandboxACEForTest(t, workspace)
	assertNoWindowsSandboxACEForTest(t, secretDir)
}

func TestWindowsSandboxRejectsWritableNetworkDisabled(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	t.Chdir(workspace)
	script := "$ErrorActionPreference='Stop'; Set-Content -LiteralPath " + psQuote(filepath.Join(workspace, "inside.txt")) + " -Value ok"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: false, Writable: true, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err == nil {
		t.Fatalf("network=false writable sandbox should fail closed, code=%d", result.ExitCode)
	}
	if !strings.Contains(err.Error(), "network=false") {
		t.Fatalf("error = %v, want network=false unsupported", err)
	}
}

func BenchmarkWindowsRestrictedWorkspaceWrite(b *testing.B) {
	if !Available() {
		b.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(b, "exit 0")
	if sh == nil {
		b.Skip("PowerShell unavailable")
	}
	opts := RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}

	b.Run("first-capability-materialization", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			workspace := b.TempDir()
			tempRoot := b.TempDir()
			spec := Spec{WritableRoots: []string{workspace}, TempDir: tempRoot, Network: true, Writable: true}
			localOpts := opts
			localOpts.Dir = workspace
			b.StartTimer()
			result, err := Run(spec, sh, localOpts)
			if err != nil || result.ExitCode != 0 {
				b.Fatalf("first launch: code=%d err=%v", result.ExitCode, err)
			}
		}
	})

	b.Run("warm-exact-ace", func(b *testing.B) {
		workspace := b.TempDir()
		tempRoot := b.TempDir()
		spec := Spec{WritableRoots: []string{workspace}, TempDir: tempRoot, Network: true, Writable: true}
		localOpts := opts
		localOpts.Dir = workspace
		if result, err := Run(spec, sh, localOpts); err != nil || result.ExitCode != 0 {
			b.Fatalf("warmup launch: code=%d err=%v", result.ExitCode, err)
		}
		b.ResetTimer()
		for range b.N {
			result, err := Run(spec, sh, localOpts)
			if err != nil || result.ExitCode != 0 {
				b.Fatalf("warm launch: code=%d err=%v", result.ExitCode, err)
			}
		}
	})
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func powershellArgvForTest(t testing.TB, command string) []string {
	t.Helper()
	for _, name := range []string{"pwsh", "powershell"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		args := []string{path, "-NoProfile", "-NonInteractive", "-Command"}
		if command != "" {
			args = append(args, command)
		}
		return args
	}
	return nil
}

func tempFileWithContent(t *testing.T, pattern string, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	return f
}

func readWholeFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func psTrySetContent(path, value string) string {
	return "try { Set-Content -LiteralPath " + psQuote(path) + " -Value " + psQuote(value) + " } catch { Write-Host $_; __winsandbox_dump_diag; exit 1 }; "
}

func psSandboxDiagnostics(root string) string {
	return "$__winsandboxDiagRoot = " + psQuote(root) + "; " +
		"function __winsandbox_dump_diag { " +
		"Write-Host '--- windows sandbox diagnostics ---'; " +
		"try { Write-Host ('USER=' + [Security.Principal.WindowsIdentity]::GetCurrent().Name) } catch {}; " +
		"try { Write-Host ('SID=' + [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) } catch {}; " +
		"Write-Host ('TEMP=' + $env:TEMP); " +
		"try { whoami /all } catch {}; " +
		"try { icacls $__winsandboxDiagRoot } catch {}; " +
		"try { icacls (Split-Path -Parent $__winsandboxDiagRoot) } catch {}; " +
		"try { icacls $env:TEMP } catch {}; " +
		"} "
}

func pathDACLSDDLForTest(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", path, err)
	}
	if sd == nil {
		return ""
	}
	return sd.String()
}

func assertNoWindowsSandboxACEForTest(t *testing.T, path string) {
	t.Helper()
	sddl := pathDACLSDDLForTest(t, path)
	for _, forbidden := range []string{
		allApplicationPackagesSID,
		allRestrictedApplicationPackagesSID,
	} {
		if strings.Contains(sddl, forbidden) {
			t.Fatalf("%s still contains sandbox SID %s: %s", path, forbidden, sddl)
		}
	}
	userSID, err := currentProcessUserSIDString()
	if err != nil {
		t.Fatalf("current user SID: %v", err)
	}
	if strings.Contains(sddl, "(D") && strings.Contains(sddl, userSID) {
		t.Fatalf("%s still contains current-user deny ACE: %s", path, sddl)
	}
}

func pathLabelSDDLForTest(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Skipf("cannot inspect integrity label for %s: %v", path, err)
		}
		t.Fatalf("read integrity label for %s: %v", path, err)
	}
	if sd == nil {
		return ""
	}
	return sd.String()
}

func sameWindowsPath(a, b string) bool {
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
