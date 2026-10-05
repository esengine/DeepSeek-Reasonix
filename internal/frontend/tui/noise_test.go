package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
)

func transcriptText(m *model) string {
	var parts []string
	for _, b := range m.scr.blocks {
		parts = append(parts, ansi.Strip(b.render(100, false)))
	}
	return strings.Join(parts, "\n")
}

func call(kind, id, name, out, err string) eventwire.Event {
	return eventwire.Event{Kind: kind, Tool: &eventwire.Tool{ID: id, Name: name, Output: out, Err: err}}
}

// The task list is drawn above the composer; the calls that keep it are not
// conversation, and 1.x leaves them out. One that failed still shows.
func TestTaskBookkeepingStaysOffTheTranscript(t *testing.T) {
	m, _ := testModel(t)
	apply(m,
		call("tool_dispatch", "t1", "todo_write", "", ""), call("tool_result", "t1", "todo_write", "ok", ""),
		call("tool_dispatch", "s1", "complete_step", "", ""), call("tool_result", "s1", "complete_step", "done", ""),
		call("tool_dispatch", "s2", "complete_step", "", ""), call("tool_result", "s2", "complete_step", "", "evidence missing"))
	got := transcriptText(m)
	if strings.Contains(got, "todo_write") || strings.Count(got, "Step") != 1 || !strings.Contains(got, "evidence missing") {
		t.Fatalf("transcript:\n%s", got)
	}
}

// A shell row shows what the command printed. The result the model reads can
// carry the host's notes after the output; those are not the command's.
func TestShellRowShowsWhatTheCommandPrinted(t *testing.T) {
	m, _ := testModel(t)
	apply(m,
		call("tool_dispatch", "b1", "bash", "", ""),
		call("tool_progress", "b1", "", "mul(3,4) = 12\n", ""),
		call("tool_result", "b1", "bash", "mul(3,4) = 12\n\nhost obligations changed:\n- settled: stale_verification (edit_file)", ""))
	got := transcriptText(m)
	if !strings.Contains(got, "mul(3,4) = 12") || strings.Contains(got, "obligations") {
		t.Fatalf("transcript:\n%s", got)
	}
}
