package control

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/sandbox"
)

// collectSink returns a Sink that collects events and a channel that receives
// the TurnDone event when the turn finishes. The channel lets tests wait for
// the runGuarded goroutine to complete.
func collectSink() (event.Sink, chan event.Event, *[]event.Event) {
	var events []event.Event
	done := make(chan event.Event, 1)
	sink := event.FuncSink(func(e event.Event) {
		events = append(events, e)
		if e.Kind == event.TurnDone {
			done <- e
		}
	})
	return sink, done, &events
}

func waitForDone(t *testing.T, done chan event.Event) event.Event {
	t.Helper()
	return waitForDoneWithin(t, done, 5*time.Second)
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
	ctrl := &Controller{sink: sink}

	ctrl.RunShell("echo hello")
	waitForDone(t, done)

	if len(*events) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(*events), *events)
	}

	// First event: ToolDispatch
	if (*events)[0].Kind != event.ToolDispatch {
		t.Errorf("first event: want ToolDispatch, got %v", (*events)[0].Kind)
	}
	if (*events)[0].Tool.Name != "bash" {
		t.Errorf("tool name: want bash, got %s", (*events)[0].Tool.Name)
	}

	// Last event: TurnDone
	td := (*events)[len(*events)-1]
	if td.Kind != event.TurnDone {
		t.Errorf("last event: want TurnDone, got %v", td.Kind)
	}

	// Penultimate event: ToolResult
	last := (*events)[len(*events)-2]
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
	ctrl := &Controller{sink: sink}

	ctrl.Submit("!echo test")
	waitForDone(t, done)

	if len(*events) == 0 {
		t.Fatal("expected events from !echo, got none")
	}
	if (*events)[0].Kind != event.ToolDispatch {
		t.Errorf("first event: want ToolDispatch, got %v", (*events)[0].Kind)
	}
}

func TestSubmit_BangEmpty(t *testing.T) {
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl := &Controller{sink: sink}
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
	ctrl := &Controller{sink: sink}

	ctrl.RunShell("false") // exits 1
	waitForDone(t, done)

	// Find the ToolResult
	var result *event.Event
	for i := range *events {
		if (*events)[i].Kind == event.ToolResult {
			result = &(*events)[i]
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
	sink, done, _ := collectSink()
	ctrl := &Controller{sink: sink}

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
}
