package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeReplayFile(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func mkLine(session, kind string, toolName, errCode string) string {
	ev := Event{
		TS: "2026-08-07T00:00:00Z", Session: session, Span: "tool", Kind: kind,
		Tool: &ToolLine{Name: toolName, DurationMs: 5},
		Err:  errCode,
	}
	if kind == "message" {
		ev.Tool = nil
		ev.Payload = "hello"
	}
	if kind == "usage" {
		ev.Tool = nil
		ev.Model = &ModelUse{Name: "m", PromptTokens: 10}
	}
	b, _ := json.Marshal(ev)
	return string(b)
}

func TestReplayReconstructsTimeline(t *testing.T) {
	dir := t.TempDir()
	// Two daily files (chronological order) + one malformed line.
	writeReplayFile(t, dir, "2026-08-07.jsonl", []string{
		mkLine("s1", "tool_use", "read_file", ""),
		mkLine("s1", "tool_use", "grep", "not_found"),
		"not-json{{{",
		mkLine("s1", "message", "", ""),
		mkLine("s1", "usage", "", ""),
	})
	writeReplayFile(t, dir, "2026-08-08.jsonl", []string{
		mkLine("s1", "tool_use", "read_file", ""),
		mkLine("s2", "tool_use", "bash", ""), // other session
		mkLine("s1", "compaction", "", ""),
	})

	res, err := Replay(dir, "")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("skipped want 1, got %d", res.Skipped)
	}
	if res.ToolCalls != 4 || res.ToolErrors != 1 || res.Messages != 1 || res.Usages != 1 || res.Compacts != 1 {
		t.Errorf("counts: calls=%d errs=%d msgs=%d usages=%d compacts=%d",
			res.ToolCalls, res.ToolErrors, res.Messages, res.Usages, res.Compacts)
	}
	// Chronological order across daily files.
	if len(res.Events) != 7 {
		t.Fatalf("events want 7, got %d", len(res.Events))
	}
	first, last := res.Events[0], res.Events[len(res.Events)-1]
	if first.Kind != "tool_use" || last.Kind != "compaction" {
		t.Errorf("order wrong: first=%s last=%s", first.Kind, last.Kind)
	}

	// Session filter.
	onlyS1, err := Replay(dir, "s1")
	if err != nil {
		t.Fatalf("Replay(s1): %v", err)
	}
	if onlyS1.ToolCalls != 3 {
		t.Errorf("s1 tool calls want 3, got %d", onlyS1.ToolCalls)
	}
	for _, e := range onlyS1.Events {
		if e.Session != "s1" {
			t.Errorf("session leak: %+v", e)
		}
	}

	// ToolCalls audit view.
	calls := res.ToolCallList()
	if len(calls) != 4 || calls[0].Name != "read_file" || calls[1].Name != "grep" {
		t.Errorf("ToolCalls = %+v", calls)
	}

	// Missing dir → empty result, no error.
	empty, err := Replay(filepath.Join(t.TempDir(), "nope"), "")
	if err != nil || len(empty.Events) != 0 {
		t.Errorf("missing dir: err=%v res=%+v", err, empty)
	}
}
