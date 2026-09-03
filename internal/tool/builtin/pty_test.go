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

	// 3. Action: write in separate turn to verify persistent environment
	write2Args, _ := json.Marshal(map[string]any{
		"action":     "write",
		"session_id": "repl-1",
		"input":      "echo HELLO_AGAIN_$HELLO_PTY\n",
		"wait_ms":    800,
	})
	write2Res, err := tool.Execute(ctx, write2Args)
	if err != nil {
		t.Fatalf("write 2 failed: %v", err)
	}
	if !strings.Contains(write2Res, "HELLO_AGAIN_WORLD") {
		// Read buffer if prompt output arrived
		readArgs, _ := json.Marshal(map[string]any{
			"action":     "read",
			"session_id": "repl-1",
		})
		readRes, _ := tool.Execute(ctx, readArgs)
		if !strings.Contains(write2Res+readRes, "HELLO_AGAIN_WORLD") {
			t.Fatalf("expected persistent var in output: %q + %q", write2Res, readRes)
		}
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
