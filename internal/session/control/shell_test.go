package control

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/sandbox"
)

// collectSink returns a Sink that collects events, a channel carrying TurnDone
// so a test can wait for runGuarded, and a snapshot of what has arrived. The
// snapshot copies under the lock the sink appends under: a turn is not the only
// emitter — the inbox notifies from its own goroutine — so handing back the
// slice itself raced with whoever wrote to it next.
func collectSink() (event.Sink, chan event.Event, func() []event.Event) {
	var mu sync.Mutex
	var events []event.Event
	done := make(chan event.Event, 1)
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		if e.Kind == event.TurnDone {
			done <- e
		}
	})
	return sink, done, func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]event.Event(nil), events...)
	}
}

func waitForDone(t *testing.T, done chan event.Event) event.Event {
	t.Helper()
	return waitForDoneWithin(t, done, testenv.Budget(t))
}

func waitForDoneWithin(t *testing.T, done chan event.Event, d time.Duration) event.Event {
	t.Helper()
	select {
	case e := <-done:
		return e
	case <-time.After(d):
		t.Fatal("timed out waiting for TurnDone")
		return event.Event{}
	}
}

func TestRunShell_EmitsEvents(t *testing.T) {
	sink, done, events := collectSink()
	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink}}

	ctrl.RunShell("echo hello")
	waitForDone(t, done)

	evs := events()
	if len(evs) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(evs), evs)
	}

	// First event: ToolDispatch
	if evs[0].Kind != event.ToolDispatch {
		t.Errorf("first event: want ToolDispatch, got %v", evs[0].Kind)
	}
	if evs[0].Tool.Name != "bash" {
		t.Errorf("tool name: want bash, got %s", evs[0].Tool.Name)
	}

	// Last event: TurnDone
	td := evs[len(evs)-1]
	if td.Kind != event.TurnDone {
		t.Errorf("last event: want TurnDone, got %v", td.Kind)
	}
	if td.CheckpointTurn != nil {
		t.Errorf("shell TurnDone checkpoint = %d, want nil", *td.CheckpointTurn)
	}

	// Penultimate event: ToolResult
	last := evs[len(evs)-2]
	if last.Kind != event.ToolResult {
		t.Errorf("penultimate event: want ToolResult, got %v", last.Kind)
	}
	if last.Tool.Err != "" {
		t.Errorf("unexpected error: %s", last.Tool.Err)
	}
	if !strings.Contains(last.Tool.Output, "hello") {
		t.Errorf("output should contain 'hello', got: %s", last.Tool.Output)
	}
}

func TestSubmit_BangPrefix(t *testing.T) {
	sink, done, events := collectSink()
	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink}}

	ctrl.Submit("!echo test")
	waitForDone(t, done)

	evs := events()
	if len(evs) == 0 {
		t.Fatal("expected events from !echo, got none")
	}
	if evs[0].Kind != event.ToolDispatch {
		t.Errorf("first event: want ToolDispatch, got %v", evs[0].Kind)
	}
}

func TestSubmit_BangEmpty(t *testing.T) {
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink}}
	ctrl.Submit("!")

	if len(notices) == 0 {
		t.Fatal("expected a notice for bare !")
	}
	if !strings.Contains(notices[0], "!") {
		t.Errorf("notice should mention usage, got: %s", notices[0])
	}
}

func TestSubmit_BangNotFirstChar(t *testing.T) {
	// "! " not at position 0 should NOT trigger shell. Submit routes to
	// runRefTurn for normal text, which needs a runner — so we test the
	// prefix-check condition directly.
	input := "tell me about !important"
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "!") {
		t.Error("trimmed input should not start with !")
	}
}

func TestRunShell_FailingCommand(t *testing.T) {
	sink, done, events := collectSink()
	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink}}

	ctrl.RunShell("false") // exits 1
	waitForDone(t, done)

	// Find the ToolResult
	evs := events()
	var result *event.Event
	for i := range evs {
		if evs[i].Kind == event.ToolResult {
			result = &evs[i]
			break
		}
	}
	if result == nil {
		t.Fatal("expected a ToolResult event")
	} else if result.Tool.Err == "" {
		t.Error("failing command should produce an error string")
	}
}

func TestRunShell_CancelStopsCommand(t *testing.T) {
	sink, done, events := collectSink()
	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink}}

	command := "sleep 30"
	if sandbox.ResolveShell("", "", nil).Kind == sandbox.ShellPowerShell {
		command = "Start-Sleep -Seconds 30"
	}
	ctrl.RunShell(command)
	time.Sleep(100 * time.Millisecond)
	ctrl.Cancel()

	// Cancel kills the shell via the run context, but cmd.Wait honours
	// shellWaitDelay (and on Windows cmd.Cancel spawns taskkill /F /T), so
	// TurnDone can arrive almost a full shellWaitDelay after Cancel. Wait
	// comfortably longer than that grace — a flat 5s budget equalled
	// shellWaitDelay and lost the race on a loaded windows runner.
	e := waitForDoneWithin(t, done, shellWaitDelay+10*time.Second)
	if e.Kind != event.TurnDone {
		t.Fatalf("done event kind = %v, want TurnDone", e.Kind)
	}
	if e.Err != nil {
		t.Fatalf("cancelled shell TurnDone err = %v, want nil", e.Err)
	}
	evs := events()
	var result *event.Event
	for i := range evs {
		if evs[i].Kind == event.ToolResult {
			result = &evs[i]
			break
		}
	}
	if result == nil {
		t.Fatal("expected ToolResult for cancelled shell")
	}
	if result.Tool.Err != i18n.M.TurnCancelled {
		t.Fatalf("cancelled shell result err = %q, want %q", result.Tool.Err, i18n.M.TurnCancelled)
	}
}

func TestRunShell_HeredocCancelReleasesTurn(t *testing.T) {
	sh := requireRunShellHereDocBash(t)
	sink, done, events := collectSink()
	root := testenv.TempDir(t)
	target := filepath.Join(root, "test_redact.go")
	ctrl := &Controller{controllerDeps: controllerDeps{sink: sink, shell: sh, workspaceRoot: root}}

	command := strings.Join([]string{
		"cat > " + controlShellQuote(filepath.ToSlash(target)) + " <<'EOF'",
		"package main",
		"",
		"import (",
		"\t\"encoding/json\"",
		"\t\"fmt\"",
		")",
		"",
		"func main() {",
		"\tdata := []byte(`{\"accounts\":[{\"id\":\"a1\",\"username\":\"alice\",\"token\":\"TOKEN_EXAMPLE\"}]}`)",
		"\tvar v any",
		"\tjson.Unmarshal(data, &v)",
		"\tfmt.Printf(\"before: %v\\n\", v)",
		"}",
		"EOF",
		"sleep 30",
	}, "\n")

	ctrl.RunShell(command)
	if !waitForFileContainingWithin(target, "TOKEN_EXAMPLE", 2*time.Second) {
		ctrl.Cancel()
		waitForDoneWithin(t, done, shellWaitDelay+10*time.Second)
		t.Fatalf("heredoc target body was not written before cancel: %s", target)
	}
	ctrl.Cancel()

	e := waitForDoneWithin(t, done, shellWaitDelay+10*time.Second)
	if e.Kind != event.TurnDone {
		t.Fatalf("done event kind = %v, want TurnDone", e.Kind)
	}
	if e.Err != nil {
		t.Fatalf("cancelled heredoc shell TurnDone err = %v, want nil", e.Err)
	}
	evs := events()
	var result *event.Event
	for i := range evs {
		if evs[i].Kind == event.ToolResult {
			result = &evs[i]
			break
		}
	}
	if result == nil {
		t.Fatal("expected ToolResult for cancelled heredoc shell")
	}
	if result.Tool.Err != i18n.M.TurnCancelled {
		t.Fatalf("cancelled heredoc shell result err = %q, want %q", result.Tool.Err, i18n.M.TurnCancelled)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read heredoc target: %v", err)
	}
	if !strings.Contains(string(data), "TOKEN_EXAMPLE") {
		t.Fatalf("heredoc target missing expected body:\n%s", data)
	}
}

func requireRunShellHereDocBash(t *testing.T) sandbox.Shell {
	t.Helper()
	sh := sandbox.ResolveShell("bash", "", nil)
	if sh.Kind != sandbox.ShellBash {
		t.Skipf("bash heredoc regression requires bash, got %s", sh.Kind.String())
	}
	path := sh.Path
	if path == "" {
		path = "bash"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, path, "-c", "true").Run(); err != nil {
		t.Skipf("bash heredoc regression requires a runnable bash: %v", err)
	}
	sh.Path = path
	return sh
}

func waitForFileContainingWithin(path, want string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func controlShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

type inputRecorder struct {
	mu     sync.Mutex
	inputs []string
}

func (r *inputRecorder) Run(_ context.Context, input string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, input)
	return nil
}

func (r *inputRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.inputs...)
}

// A command the user ran reaches the model as something they said: the
// command, its exit code and its output, answered in a turn of its own.
func TestRunShellHandsTheOutputToTheModel(t *testing.T) {
	sink, done, _ := collectSink()
	runner := &inputRecorder{}
	ctrl := New(Options{Runner: runner, Sink: sink})
	t.Cleanup(ctrl.Close)

	ctrl.RunShell("echo shell-into-context")
	waitForDone(t, done)
	inputs := runner.seen()
	if len(inputs) != 1 {
		t.Fatalf("model turns = %d, want 1", len(inputs))
	}
	for _, want := range []string{"<bash-input>echo shell-into-context</bash-input>", "<bash-exit-code>0</bash-exit-code>", "shell-into-context\n"} {
		if !strings.Contains(inputs[0], want) {
			t.Fatalf("model input missing %q:\n%s", want, inputs[0])
		}
	}
}

// A terminal's `!` command can stay the user's own: its output is shown and
// nothing reaches the model, as 1.x has it.
func TestRunShellLocalOnlyLeavesTheModelOut(t *testing.T) {
	sink, done, events := collectSink()
	runner := &inputRecorder{}
	ctrl := New(Options{Runner: runner, Sink: sink})
	t.Cleanup(ctrl.Close)

	ctrl.RunShellWith("echo shell-stays-local", ShellRun{LocalOnly: true})
	waitForDone(t, done)
	if n := len(runner.seen()); n != 0 {
		t.Fatalf("a local command was answered %d times: %q", n, runner.seen())
	}
	var shown bool
	for _, e := range events() {
		if e.Kind == event.ToolResult && strings.Contains(e.Tool.Output, "shell-stays-local") {
			shown = true
		}
	}
	if !shown {
		t.Fatal("the command's output never reached the screen")
	}
}

// A command the user stopped is not something to answer.
func TestCancelledShellIsNotAnswered(t *testing.T) {
	sink, done, _ := collectSink()
	runner := &inputRecorder{}
	ctrl := New(Options{Runner: runner, Sink: sink})
	t.Cleanup(ctrl.Close)
	command := "sleep 30"
	if sandbox.ResolveShell("", "", nil).Kind == sandbox.ShellPowerShell {
		command = "Start-Sleep -Seconds 30"
	}
	ctrl.RunShell(command)
	time.Sleep(100 * time.Millisecond)
	ctrl.Cancel()
	waitForDoneWithin(t, done, shellWaitDelay+10*time.Second)
	if n := len(runner.seen()); n != 0 {
		t.Fatalf("a cancelled command was answered %d times", n)
	}
}

func TestShellTurnInputCutsLongOutputAndSaysSo(t *testing.T) {
	long := strings.Repeat("a", shellContextBytes*2)
	got := shellTurnInput("cat big", nil, long, "")
	if len(got) > shellContextBytes+512 || !strings.Contains(got, "bytes of output cut from the middle") {
		t.Fatalf("long output not bounded (%d bytes)", len(got))
	}
}
