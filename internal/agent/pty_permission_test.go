package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/pty"
	"reasonix/internal/tool"
)

func TestPTYWriteRespectsBashPermissionDeny(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tool.NewRegistry()
	for _, b := range tool.Builtins() {
		reg.Add(b)
	}

	mgr := pty.NewManager(tmpDir)
	defer mgr.CloseAll()

	// Gate with explicit deny for "Bash(git push*)" and "Bash(rm -rf*)"
	policy := permission.New("allow", nil, nil, []string{"Bash(git push*)", "Bash(rm -rf*)"})
	gate := permission.NewGate(policy, nil)

	opts := Options{
		Gate: gate,
		PTY:  mgr,
	}

	ag := New(&fakeProvider{}, reg, NewSession("sys"), opts, event.Discard)

	// 1. Attempt pty.start with "git push origin main" — should be denied by Bash(git push:*) policy
	callStartDeny := provider.ToolCall{
		ID:   "call-start-deny",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":  "start",
				"command": "git push origin main",
			})
			return string(b)
		}(),
	}
	outcomeStart := ag.executeOne(context.Background(), &turnRuntime{}, callStartDeny)
	if !outcomeStart.blocked {
		t.Fatalf("expected pty.start with git push to be blocked by permission policy, got: %+v", outcomeStart)
	}

	// 2. Start legitimate shell session
	callStartOK := provider.ToolCall{
		ID:   "call-start-ok",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "interactive-sess",
			})
			return string(b)
		}(),
	}
	outcomeStartOK := ag.executeOne(context.Background(), &turnRuntime{}, callStartOK)
	if outcomeStartOK.blocked {
		t.Fatalf("expected legitimate pty.start to succeed, got blocked: %+v", outcomeStartOK)
	}

	// Verify session is alive and running
	sess, err := mgr.Get("interactive-sess")
	if err != nil || !sess.IsRunning() {
		t.Fatalf("expected interactive-sess to be running, got err: %v", err)
	}

	// 3. Test P0-1: A blocked pty.start must NOT send Ctrl+C to existing running session
	callStartDeny2 := provider.ToolCall{
		ID:   "call-start-deny-2",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "interactive-sess",
				"command":    "git push origin main",
			})
			return string(b)
		}(),
	}
	outcomeStartDeny2 := ag.executeOne(context.Background(), &turnRuntime{}, callStartDeny2)
	if !outcomeStartDeny2.blocked {
		t.Fatalf("expected second start to be blocked")
	}
	// Give a moment and verify interactive-sess is STILL running (no side-effect kill)
	time.Sleep(50 * time.Millisecond)
	if !sess.IsRunning() {
		t.Fatalf("existing running session was terminated by an unrelated blocked start call!")
	}

	// 4. Test pty.write_line with denied command "git push origin main"
	callWriteLineDeny := provider.ToolCall{
		ID:   "call-write-line-deny",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write_line",
				"session_id": "interactive-sess",
				"command":    "git push origin main",
			})
			return string(b)
		}(),
	}
	outcomeWriteLine := ag.executeOne(context.Background(), &turnRuntime{}, callWriteLineDeny)
	if !outcomeWriteLine.blocked {
		t.Fatalf("expected pty.write_line with git push to be blocked by permission policy, got: %+v", outcomeWriteLine)
	}
	if !strings.Contains(outcomeWriteLine.output, "blocked") {
		t.Fatalf("expected blocked output message, got: %q", outcomeWriteLine.output)
	}

	// 5. Test pty.write sends raw keystrokes and is allowed
	callWriteRaw := provider.ToolCall{
		ID:   "call-write-raw",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "interactive-sess",
				"input":      "echo HELLO_SAFE\n",
			})
			return string(b)
		}(),
	}
	outcomeWriteRaw := ag.executeOne(context.Background(), &turnRuntime{}, callWriteRaw)
	if outcomeWriteRaw.blocked {
		t.Fatalf("expected raw write to be allowed, got blocked: %+v", outcomeWriteRaw)
	}

	// 5b. Test pty.write with denied command containing newline is blocked (cannot bypass Bash deny)
	callWriteDeny := provider.ToolCall{
		ID:   "call-write-deny",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "interactive-sess",
				"input":      "git push origin main\n",
			})
			return string(b)
		}(),
	}
	outcomeWriteDeny := ag.executeOne(context.Background(), &turnRuntime{}, callWriteDeny)
	if !outcomeWriteDeny.blocked {
		t.Fatalf("expected pty.write with git push to be blocked by permission policy, got: %+v", outcomeWriteDeny)
	}

	// 6. Test pty.read — should be read-only and allowed
	callRead := provider.ToolCall{
		ID:   "call-read",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "read",
				"session_id": "interactive-sess",
			})
			return string(b)
		}(),
	}
	outcomeRead := ag.executeOne(context.Background(), &turnRuntime{}, callRead)
	if outcomeRead.blocked {
		t.Fatalf("expected pty.read to be allowed without write-block, got blocked: %+v", outcomeRead)
	}
}

func TestPTYBasePermissionDeny(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tool.NewRegistry()
	for _, b := range tool.Builtins() {
		reg.Add(b)
	}

	mgr := pty.NewManager(tmpDir)
	defer mgr.CloseAll()

	// Gate with explicit deny for "pty" tool
	policy := permission.New("allow", nil, nil, []string{"pty"})
	gate := permission.NewGate(policy, nil)

	opts := Options{
		Gate: gate,
		PTY:  mgr,
	}

	ag := New(&fakeProvider{}, reg, NewSession("sys"), opts, event.Discard)

	// 1. Attempt pty.start when pty is denied — must be blocked even if command is harmless "bash"
	callStart := provider.ToolCall{
		ID:   "call-start",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":  "start",
				"command": "bash",
			})
			return string(b)
		}(),
	}
	outcomeStart := ag.executeOne(context.Background(), &turnRuntime{}, callStart)
	if !outcomeStart.blocked {
		t.Fatalf("expected pty.start to be blocked by PTY base deny rule, got: %+v", outcomeStart)
	}

	// 2. Attempt pty.write_line when pty is denied — must be blocked by PTY base deny
	callWriteLine := provider.ToolCall{
		ID:   "call-write-line",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":  "write_line",
				"command": "ls -la",
			})
			return string(b)
		}(),
	}
	outcomeWriteLine := ag.executeOne(context.Background(), &turnRuntime{}, callWriteLine)
	if !outcomeWriteLine.blocked {
		t.Fatalf("expected pty.write_line to be blocked by PTY base deny rule, got: %+v", outcomeWriteLine)
	}
}

// TestPTYWriteSplitCommandRespectsBashPermissionDeny verifies that splitting a
// denied shell command across two consecutive pty.write tool calls cannot bypass
// the Bash permission gate.  The first call ("git ") carries no newline and must
// pass through (it is just raw interactive text).  The second call
// ("push origin main\n") completes the command line with a trailing newline, so
// the gate must classify it as "bash: git push origin main" and deny it.
func TestPTYWriteSplitCommandRespectsBashPermissionDeny(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tool.NewRegistry()
	for _, b := range tool.Builtins() {
		reg.Add(b)
	}

	mgr := pty.NewManager(tmpDir)
	defer mgr.CloseAll()

	policy := permission.New("allow", nil, nil, []string{"Bash(git push*)"})
	gate := permission.NewGate(policy, nil)

	opts := Options{
		Gate: gate,
		PTY:  mgr,
	}

	ag := New(&fakeProvider{}, reg, NewSession("sys"), opts, event.Discard)

	// Start a harmless session first.
	callStart := provider.ToolCall{
		ID:   "call-split-start",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "split-test",
			})
			return string(b)
		}(),
	}
	if out := ag.executeOne(context.Background(), &turnRuntime{}, callStart); out.blocked {
		t.Fatalf("expected legitimate start to succeed, got blocked: %+v", out)
	}

	// First fragment: "git " — no newline, should be allowed (raw text, not a complete command).
	callWritePart1 := provider.ToolCall{
		ID:   "call-split-write-1",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "split-test",
				"input":      "git ",
			})
			return string(b)
		}(),
	}
	out1 := ag.executeOne(context.Background(), &turnRuntime{}, callWritePart1)
	if out1.blocked {
		t.Fatalf("first fragment 'git ' without newline should be allowed, got blocked: %+v", out1)
	}

	// Second fragment: "push origin main\n" — contains newline, completes the command.
	// Gate must now classify the command as "bash: push origin main" and deny it.
	callWritePart2 := provider.ToolCall{
		ID:   "call-split-write-2",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "split-test",
				"input":      "push origin main\n",
			})
			return string(b)
		}(),
	}
	out2 := ag.executeOne(context.Background(), &turnRuntime{}, callWritePart2)
	if !out2.blocked {
		t.Fatalf("second fragment 'push origin main\\n' should be blocked by Bash deny rule, got: %+v", out2)
	}
	if !strings.Contains(out2.output, "blocked") {
		t.Fatalf("expected blocked message in output, got: %q", out2.output)
	}
}

