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

// TestPTYWriteDeniedCompletionPreservesPendingInput specifically tests against the
// secondary bypass attack where:
//  1. write("git ") is allowed and lands in PTY (pending = "git ")
//  2. write("push origin main\n") is denied by Bash(git push*)
//  3. A naive accumulator might reset on deny, leaving PTY with "git " but
//     pending = "". Then a subsequent write("push origin main\n") would only
//     see "push origin main" at the gate (not matching git push*), successfully
//     bypassing the deny rule!
//
// With the non-destructive preview + commit-on-allow architecture, the pending
// prefix is strictly preserved across denials, blocking any subsequent attempts.
func TestPTYWriteDeniedCompletionPreservesPendingInput(t *testing.T) {
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

	// Start session
	callStart := provider.ToolCall{
		ID:   "call-sec-start",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "sec-attack-sess",
			})
			return string(b)
		}(),
	}
	if out := ag.executeOne(context.Background(), &turnRuntime{}, callStart); out.blocked {
		t.Fatalf("expected start to succeed, got blocked: %+v", out)
	}

	sess, err := mgr.Get("sec-attack-sess")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	// 1. Step 1: write("git ") — allowed, lands in PTY
	call1 := provider.ToolCall{
		ID:   "call-step-1",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "sec-attack-sess",
				"input":      "git ",
			})
			return string(b)
		}(),
	}
	out1 := ag.executeOne(context.Background(), &turnRuntime{}, call1)
	if out1.blocked {
		t.Fatalf("step 1 write('git ') should be allowed: %+v", out1)
	}
	if got := sess.PendingInput(); got != "git " {
		t.Fatalf("expected pending input to be 'git ', got: %q", got)
	}

	// 2. Step 2: write("push origin main\n") — DENIED
	call2 := provider.ToolCall{
		ID:   "call-step-2",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "sec-attack-sess",
				"input":      "push origin main\n",
			})
			return string(b)
		}(),
	}
	out2 := ag.executeOne(context.Background(), &turnRuntime{}, call2)
	if !out2.blocked {
		t.Fatalf("step 2 write('push origin main\\n') must be blocked by Bash(git push*): %+v", out2)
	}
	// CRITICAL ASSERTION: The pending buffer must STILL be "git "! Deny must NOT reset it.
	if got := sess.PendingInput(); got != "git " {
		t.Fatalf("denial must NOT reset pending input! Expected 'git ', got: %q", got)
	}

	// 3. Step 3: Attacker retries write("push origin main\n") — MUST STILL BE DENIED!
	call3 := provider.ToolCall{
		ID:   "call-step-3-attack-retry",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "sec-attack-sess",
				"input":      "push origin main\n",
			})
			return string(b)
		}(),
	}
	out3 := ag.executeOne(context.Background(), &turnRuntime{}, call3)
	if !out3.blocked {
		t.Fatalf("step 3 secondary bypass attempt must be BLOCKED by preserved 'git ' prefix: %+v", out3)
	}

	// 4. Step 4: write("\x03") (Ctrl+C) cancels the unsubmitted line in PTY
	callCancel := provider.ToolCall{
		ID:   "call-step-4-ctrl-c",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "sec-attack-sess",
				"input":      "\x03",
			})
			return string(b)
		}(),
	}
	outCancel := ag.executeOne(context.Background(), &turnRuntime{}, callCancel)
	if outCancel.blocked {
		t.Fatalf("Ctrl+C must be allowed: %+v", outCancel)
	}
	if got := sess.PendingInput(); got != "" {
		t.Fatalf("Ctrl+C must clear pending line buffer, got: %q", got)
	}
}

// TestPTYWriteMultiCommandRespectsBashPermissionDeny verifies that sending multiple
// newline-terminated commands in a single write call (e.g. "echo ok\ngit push origin main\n")
// checks EVERY command against the Bash gate. If any command is denied (such as git push),
// the entire call is blocked, and no commands are written to the PTY.
func TestPTYWriteMultiCommandRespectsBashPermissionDeny(t *testing.T) {
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

	// Start session
	callStart := provider.ToolCall{
		ID:   "call-multi-start",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "multi-cmd-sess",
			})
			return string(b)
		}(),
	}
	if out := ag.executeOne(context.Background(), &turnRuntime{}, callStart); out.blocked {
		t.Fatalf("expected start to succeed: %+v", out)
	}

	sess, err := mgr.Get("multi-cmd-sess")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	// Drain initial prompt output
	_ = sess.Read(4096)

	// Attempt multi-command write: "echo ok\ngit push origin main\n"
	// Line 1 is harmless "echo ok", Line 2 is forbidden "git push origin main"
	callMulti := provider.ToolCall{
		ID:   "call-multi-write",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "multi-cmd-sess",
				"input":      "echo ok\ngit push origin main\n",
			})
			return string(b)
		}(),
	}
	outMulti := ag.executeOne(context.Background(), &turnRuntime{}, callMulti)
	if !outMulti.blocked {
		t.Fatalf("expected multi-command write containing git push to be blocked, got: %+v", outMulti)
	}
	if !strings.Contains(outMulti.output, "blocked") {
		t.Fatalf("expected blocked message in output, got: %q", outMulti.output)
	}

	// Verify nothing was written to PTY (the harmless 'echo ok' must NOT have been executed either)
	time.Sleep(50 * time.Millisecond)
	unread := sess.Read(4096)
	if strings.Contains(unread, "echo ok") || strings.Contains(unread, "git push") {
		t.Fatalf("expected PTY to receive no commands from a blocked multi-command write, got output: %q", unread)
	}
}

// TestPTYWriteLineRejectsPendingRawInput verifies that an attacker cannot bypass
// Bash permission rules by splitting across different actions:
//  1. pty.write("git ") leaves uncommitted "git " sitting on the terminal prompt.
//  2. pty.write_line("push origin main") attempts to complete and submit it.
//
// Both the Gate (evaluating pendingInput + command) and the execution layer
// (enforcing clean-line invariant) block this attempt, preventing git push from executing.
func TestPTYWriteLineRejectsPendingRawInput(t *testing.T) {
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

	// Start session
	callStart := provider.ToolCall{
		ID:   "call-cross-start",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "start",
				"session_id": "cross-action-sess",
			})
			return string(b)
		}(),
	}
	if out := ag.executeOne(context.Background(), &turnRuntime{}, callStart); out.blocked {
		t.Fatalf("expected start to succeed: %+v", out)
	}

	sess, err := mgr.Get("cross-action-sess")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	_ = sess.Read(4096)

	// 1. Step 1: write("git ") — allowed, leaves "git " in pending buffer
	call1 := provider.ToolCall{
		ID:   "call-cross-write",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "cross-action-sess",
				"input":      "git ",
			})
			return string(b)
		}(),
	}
	out1 := ag.executeOne(context.Background(), &turnRuntime{}, call1)
	if out1.blocked {
		t.Fatalf("step 1 write('git ') should be allowed: %+v", out1)
	}
	if got := sess.PendingInput(); got != "git " {
		t.Fatalf("expected pending input to be 'git ', got: %q", got)
	}

	// 2. Step 2: write_line("push origin main") — MUST BE BLOCKED!
	call2 := provider.ToolCall{
		ID:   "call-cross-write-line",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write_line",
				"session_id": "cross-action-sess",
				"command":    "push origin main",
			})
			return string(b)
		}(),
	}
	out2 := ag.executeOne(context.Background(), &turnRuntime{}, call2)
	if !out2.blocked {
		t.Fatalf("expected cross-action write_line completing git push to be blocked, got: %+v", out2)
	}
	if !strings.Contains(out2.output, "blocked") {
		t.Fatalf("expected blocked message in output, got: %q", out2.output)
	}

	// 3. Step 3: Harmless write_line("echo hello") must ALSO be rejected because pending raw input exists
	call3 := provider.ToolCall{
		ID:   "call-cross-harmless-line",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write_line",
				"session_id": "cross-action-sess",
				"command":    "echo hello",
			})
			return string(b)
		}(),
	}
	out3 := ag.executeOne(context.Background(), &turnRuntime{}, call3)
	// Either blocked by gate or execution rejected due to pending input
	if !out3.blocked && !strings.Contains(out3.output, "pending unsubmitted raw input") && !strings.Contains(out3.output, "blocked") {
		t.Fatalf("write_line on dirty prompt with pending input must be rejected, got: %+v", out3)
	}

	// 4. Step 4: write("\x03") (Ctrl+C) cancels the line
	callClear := provider.ToolCall{
		ID:   "call-cross-clear",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write",
				"session_id": "cross-action-sess",
				"input":      "\x03",
			})
			return string(b)
		}(),
	}
	outClear := ag.executeOne(context.Background(), &turnRuntime{}, callClear)
	if outClear.blocked {
		t.Fatalf("Ctrl+C clear should be allowed: %+v", outClear)
	}
	if got := sess.PendingInput(); got != "" {
		t.Fatalf("expected pending input to be empty after Ctrl+C, got: %q", got)
	}

	// 5. Step 5: Now write_line on clean prompt succeeds normally
	call5 := provider.ToolCall{
		ID:   "call-cross-clean-line",
		Name: "pty",
		Arguments: func() string {
			b, _ := json.Marshal(map[string]any{
				"action":     "write_line",
				"session_id": "cross-action-sess",
				"command":    "echo CLEAN_LINE_OK",
			})
			return string(b)
		}(),
	}
	out5 := ag.executeOne(context.Background(), &turnRuntime{}, call5)
	if out5.blocked {
		t.Fatalf("write_line on clean prompt should succeed, got blocked: %+v", out5)
	}
	if !strings.Contains(out5.output, "CLEAN_LINE_OK") {
		t.Fatalf("expected output to contain CLEAN_LINE_OK, got: %q", out5.output)
	}
}




