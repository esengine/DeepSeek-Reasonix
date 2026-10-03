package control

import (
	"strings"
	"testing"

	"reasonix/internal/contract/event"
)

func TestRemoteSlashIsHandledByController(t *testing.T) {
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})
	if !c.managementNotice("/remote") {
		t.Fatal("/remote was not handled")
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %v", notices)
	}
}

func TestRetiredPresetCommandsNeverChangeRuntime(t *testing.T) {
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})
	before := c.AgentPreset()
	for _, cmd := range []string{"/preset delivery", "/work-mode balanced", "/profile light"} {
		if !c.managementNotice(cmd) {
			t.Fatalf("not handled: %s", cmd)
		}
	}
	for _, notice := range notices {
		if !strings.Contains(notice, "Execution modes have been retired.") {
			t.Fatalf("notice: %s", notice)
		}
	}
	if got := c.AgentPreset(); got != before {
		t.Fatalf("retired command changed preset from %s to %s", before, got)
	}
}
