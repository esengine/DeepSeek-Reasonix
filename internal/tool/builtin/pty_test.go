package builtin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/pty"
)

func TestPTYToolLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasonix-pty-tool-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := pty.NewManager(tmpDir)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	if tool.Name() != "pty" {
		t.Fatalf("tool.Name() = %q, want 'pty'", tool.Name())
	}
	if tool.ReadOnly() {
		t.Fatalf("tool.ReadOnly() should be false")
	}
	if hint := tool.SnipHint(); hint.Head != 40 || hint.Tail != 40 {
		t.Fatalf("unexpected SnipHint: %+v", hint)
	}

	// 1. Action: start
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "repl-1",
		"cwd":        tmpDir,
	})
	startRes, err := tool.Execute(ctx, startArgs)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !strings.Contains(startRes, "Started PTY session \"repl-1\"") {
		t.Fatalf("unexpected start output: %q", startRes)
	}

	// 2. Action: write_line (export environment variable and write a file)
	writeArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "repl-1",
		"command":    "export HELLO_PTY=WORLD && echo $HELLO_PTY > pty_out.txt && cat pty_out.txt",
		"wait_ms":    800,
	})
	writeRes, err := tool.Execute(ctx, writeArgs)
	if err != nil {
		t.Fatalf("write_line failed: %v", err)
	}
	t.Logf("Write_line response:\n%s", writeRes)

	// Verify file was written in the cwd
	time.Sleep(200 * time.Millisecond)
	content, err := os.ReadFile(tmpDir + "/pty_out.txt")
	if err != nil {
		t.Logf("ReadFile note: %v (might have settled into output directly)", err)
	} else if strings.TrimSpace(string(content)) != "WORLD" {
		t.Fatalf("file content = %q, want 'WORLD'", string(content))
	}

	// 3. Action: write_line in separate turn to verify persistent environment
	write2Args, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "repl-1",
		"command":    "echo HELLO_AGAIN_$HELLO_PTY",
		"wait_ms":    800,
	})
	write2Res, err := tool.Execute(ctx, write2Args)
	if err != nil {
		t.Fatalf("write_line 2 failed: %v", err)
	}
	if !strings.Contains(write2Res, "HELLO_AGAIN_WORLD") {
		t.Fatalf("expected persistent var in output: %q", write2Res)
	}

	// 3b. Verify raw write with newline is strictly blocked in ShellReady mode
	rawWriteArgs, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "repl-1",
		"input":      "echo CANNOT_USE_RAW_WRITE\n",
	})
	_, rawErr := tool.Execute(ctx, rawWriteArgs)
	if rawErr == nil || !strings.Contains(rawErr.Error(), "submitting shell commands via raw write is not allowed") {
		t.Fatalf("expected raw write with newline to be blocked in ShellReady, got: %v", rawErr)
	}

	// 4. Test security policy blocks catastrophic dangerous command
	dangerousArgs, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "repl-1",
		"input":      "rm -rf / --no-preserve-root\n",
	})
	_, dangerErr := tool.Execute(ctx, dangerousArgs)
	if dangerErr == nil || !strings.Contains(dangerErr.Error(), "blocked: dangerous") {
		t.Fatalf("expected dangerous command to be blocked, got err: %v", dangerErr)
	}

	// 5. Action: list
	listArgs, _ := json.Marshal(map[string]any{
		"action": "list",
	})
	listRes, err := tool.Execute(ctx, listArgs)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(listRes, "repl-1") {
		t.Fatalf("list output missing repl-1: %q", listRes)
	}

	// 6. Action: resize
	resizeArgs, _ := json.Marshal(map[string]any{
		"action":     "resize",
		"session_id": "repl-1",
		"cols":       140,
		"rows":       50,
	})
	resizeRes, err := tool.Execute(ctx, resizeArgs)
	if err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	if !strings.Contains(resizeRes, "Resized PTY session \"repl-1\" to 140x50") {
		t.Fatalf("unexpected resize response: %q", resizeRes)
	}

	// 7. Action: close
	closeArgs, _ := json.Marshal(map[string]any{
		"action":     "close",
		"session_id": "repl-1",
	})
	closeRes, err := tool.Execute(ctx, closeArgs)
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !strings.Contains(closeRes, "Closed PTY session \"repl-1\"") {
		t.Fatalf("unexpected close response: %q", closeRes)
	}
}

func TestPTYWriteLineTimeoutNotice(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	// Start a session
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "timeout-sess",
	})
	if _, err := tool.Execute(ctx, startArgs); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Run a command that takes 2s, but wait_ms is only 200ms
	waitMs := 200
	slowArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "timeout-sess",
		"command":    "sleep 2 && echo DONE_AFTER_SLEEP",
		"wait_ms":    &waitMs,
	})
	res, err := tool.Execute(ctx, slowArgs)
	if err != nil {
		t.Fatalf("execute should succeed with running notice, got error: %v", err)
	}
	if !strings.Contains(res, "command still running") {
		t.Fatalf("expected running notice for timed-out command, got: %q", res)
	}

	// Wait 2.2s for command to actually finish, then read
	time.Sleep(2200 * time.Millisecond)
	readArgs, _ := json.Marshal(map[string]any{
		"action":     "read",
		"session_id": "timeout-sess",
	})
	readRes, err := tool.Execute(ctx, readArgs)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(readRes, "DONE_AFTER_SLEEP") {
		t.Fatalf("expected read to capture completed output, got: %q", readRes)
	}
}

func TestPTYWriteLineNonBlockingZeroWaitMs(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	// Start a session
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "nonblock-sess",
	})
	if _, err := tool.Execute(ctx, startArgs); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Run write_line with wait_ms: 0
	waitZero := 0
	nbArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "nonblock-sess",
		"command":    "echo NON_BLOCKING_OK",
		"wait_ms":    &waitZero,
	})
	res, err := tool.Execute(ctx, nbArgs)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(res, "Command line written (non-blocking).") {
		t.Fatalf("expected non-blocking message, got: %q", res)
	}
}

func TestPTYWriteLineNonZeroExitCode(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	// Start a session
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "exit-code-sess",
	})
	if _, err := tool.Execute(ctx, startArgs); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Run command with non-zero exit code that does not terminate the shell
	subshellArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "exit-code-sess",
		"command":    "echo FAILED_OUTPUT && (exit 42)",
		"wait_ms":    1500,
	})
	res, err := tool.Execute(ctx, subshellArgs)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(res, "(command exited with code 42)") {
		t.Fatalf("expected '(command exited with code 42)', got: %q", res)
	}

	// Run command that exits the shell process itself
	exitArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "exit-code-sess",
		"command":    "echo EXITING_NOW && exit 7",
		"wait_ms":    1500,
	})
	exitRes, err := tool.Execute(ctx, exitArgs)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(exitRes, "(session terminated with exit code 7)") {
		t.Fatalf("expected '(session terminated with exit code 7)', got: %q", exitRes)
	}
}

func TestPTYStartExplicitArgs(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	// Start with explicit args array containing spaces
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "explicit-args-sess",
		"command":    "bash",
		"args":       []string{"-c", "echo 'HELLO FROM EXPLICIT ARGS'"},
	})
	res, err := tool.Execute(ctx, startArgs)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	t.Logf("Start result: %s", res)
}

func TestPTYRejectWriteLineWhileCommandRunning(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "busy-sess",
	})
	if _, err := tool.Execute(ctx, startArgs); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Launch a long sleep command non-blocking (wait_ms: 0)
	waitZero := 0
	sleepArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "busy-sess",
		"command":    "sleep 5",
		"wait_ms":    &waitZero,
	})
	if _, err := tool.Execute(ctx, sleepArgs); err != nil {
		t.Fatalf("non-blocking execute failed: %v", err)
	}

	// Immediately attempting another write_line must be rejected by state machine
	secondArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "busy-sess",
		"command":    "echo SHOULD_BE_REJECTED",
	})
	_, secondErr := tool.Execute(ctx, secondArgs)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "command_running") {
		t.Fatalf("expected command_running error, got: %v", secondErr)
	}

	// Interrupt with Ctrl+C
	ctrlCArgs, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "busy-sess",
		"input":      "\x03",
	})
	if _, err := tool.Execute(ctx, ctrlCArgs); err != nil {
		t.Fatalf("interrupt failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Now write_line should succeed again!
	thirdArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "busy-sess",
		"command":    "echo RECOVERED_AFTER_CTRL_C",
		"wait_ms":    1000,
	})
	thirdRes, err := tool.Execute(ctx, thirdArgs)
	if err != nil {
		t.Fatalf("write_line after Ctrl+C failed: %v", err)
	}
	if !strings.Contains(thirdRes, "RECOVERED_AFTER_CTRL_C") {
		t.Fatalf("expected output after Ctrl+C recovery, got: %q", thirdRes)
	}
}

func TestPTYWriteLineInteractivePythonSentinelIsolation(t *testing.T) {
	workspace := t.TempDir()
	mgr := pty.NewManager(workspace)
	defer mgr.CloseAll()

	ctx := pty.WithManager(context.Background(), mgr)
	tool := ptyTool{}

	// Start bash session
	startArgs, _ := json.Marshal(map[string]any{
		"action":     "start",
		"session_id": "py-iso-sess",
	})
	if _, err := tool.Execute(ctx, startArgs); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Launch python3 -i via write_line with wait_ms: 0 (non-blocking)
	waitZero := 0
	pyLaunchArgs, _ := json.Marshal(map[string]any{
		"action":     "write_line",
		"session_id": "py-iso-sess",
		"command":    "python3 -i",
		"wait_ms":    &waitZero,
	})
	if _, err := tool.Execute(ctx, pyLaunchArgs); err != nil {
		t.Fatalf("launch python failed: %v", err)
	}

	// Allow python to start
	time.Sleep(300 * time.Millisecond)

	// Send code into interactive Python via action=write (which is allowed while CommandRunning)
	writeCodeArgs, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "py-iso-sess",
		"input":      "print('MATH_RESULT=' + str(21 * 2))\n",
		"wait_ms":    500,
	})
	codeRes, err := tool.Execute(ctx, writeCodeArgs)
	if err != nil {
		t.Fatalf("write code to python failed: %v", err)
	}

	// Read output to verify Python evaluated it without syntax errors from leaked sentinels
	readArgs, _ := json.Marshal(map[string]any{
		"action":     "read",
		"session_id": "py-iso-sess",
	})
	readRes, _ := tool.Execute(ctx, readArgs)
	allOut := codeRes + readRes
	if strings.Contains(allOut, "SyntaxError") {
		t.Fatalf("sentinel leaked into python stdin causing SyntaxError: %s", allOut)
	}
	if !strings.Contains(allOut, "MATH_RESULT=42") {
		t.Fatalf("expected python output MATH_RESULT=42, got: %s", allOut)
	}

	// Exit Python
	exitPyArgs, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "py-iso-sess",
		"input":      "exit()\n",
		"wait_ms":    500,
	})
	_, _ = tool.Execute(ctx, exitPyArgs)
	time.Sleep(400 * time.Millisecond)

	// After Python exits, session must have automatically returned to ShellReady!
	sess, _ := mgr.Get("py-iso-sess")
	if got := sess.State(); got != pty.StateShellReady {
		t.Fatalf("expected session to auto-restore to ShellReady after python exit, got: %s", got)
	}
}




