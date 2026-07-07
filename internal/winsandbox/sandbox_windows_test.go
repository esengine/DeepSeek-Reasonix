//go:build windows

package winsandbox

import (
	"context"
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

func TestWindowsSandboxStartupInfoHidesWindowAndKeepsStdHandles(t *testing.T) {
	handles := [3]windows.Handle{11, 12, 13}
	si := windowsSandboxStartupInfo(handles, nil)
	if si.StartupInfo.Cb == 0 {
		t.Fatal("startup info size was not initialized")
	}
	if si.StartupInfo.Flags&windows.STARTF_USESTDHANDLES == 0 {
		t.Fatalf("startup flags %#x missing STARTF_USESTDHANDLES", si.StartupInfo.Flags)
	}
	if si.StartupInfo.Flags&windows.STARTF_USESHOWWINDOW == 0 {
		t.Fatalf("startup flags %#x missing STARTF_USESHOWWINDOW", si.StartupInfo.Flags)
	}
	if si.StartupInfo.ShowWindow != windows.SW_HIDE {
		t.Fatalf("ShowWindow = %d, want SW_HIDE", si.StartupInfo.ShowWindow)
	}
	if si.StartupInfo.StdInput != handles[0] || si.StartupInfo.StdOutput != handles[1] || si.StartupInfo.StdErr != handles[2] {
		t.Fatalf("std handles = (%v,%v,%v), want %v", si.StartupInfo.StdInput, si.StartupInfo.StdOutput, si.StartupInfo.StdErr, handles)
	}
}

func TestWindowsSandboxSystemCommandsAreHidden(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
}

func TestWindowsSandboxReadOnlyAllowsReadsAndDeniesWrites(t *testing.T) {
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
		"try { Set-Content -LiteralPath " + psQuote(writtenFile) + " -Value nope; exit 9 } catch { exit 0 }"
	result, err := Run(Spec{WritableRoots: []string{workspace}, Network: true, Writable: false, TempPrefix: "windows-sandbox-test-"}, append(sh, script), RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
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

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func powershellArgvForTest(t *testing.T, command string) []string {
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

func sameWindowsPath(a, b string) bool {
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
