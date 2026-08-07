package flywheel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestDigest(t *testing.T) {
	short := "hello world"
	if got := Digest(short); got != short {
		t.Fatalf("short input should pass through, got %q", got)
	}
	long := strings.Repeat("x", maxDigestLen+50)
	got := Digest(long)
	if len(got) != maxDigestLen+len("…") {
		t.Fatalf("digest length = %d, want %d", len(got), maxDigestLen+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("digest should end with ellipsis, got %q", got)
	}
}

func TestErrCode(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"permission denied":    "permission",
		"context deadline exceeded": "timeout",
		"no such file or directory": "not_found",
		"main.go:3: syntax error":   "compile",
		"boom":                      "error",
	}
	for in, want := range cases {
		if got := ErrCode(in); got != want {
			t.Errorf("ErrCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSinkSnapshotToolCall(t *testing.T) {
	dir := t.TempDir()
	var inner []event.Event
	var innerSink event.Sink = event.FuncSink(func(e event.Event) { inner = append(inner, e) })
	s := NewSink(innerSink, dir, "sess-1")
	defer s.Close()

	s.Emit(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{Name: "edit_file", Args: "path=a.go:edit(~40B)"},
	})
	s.Emit(event.Event{
		Kind:       event.ToolResult,
		Tool:       event.Tool{Name: "edit_file", Args: "path=a.go", Output: "diff 2 files", DurationMs: 412, Err: "permission denied"},
	})

	// Forwarding must be unchanged.
	if len(inner) != 2 || inner[0].Kind != event.ToolDispatch || inner[1].Tool.DurationMs != 412 {
		t.Fatalf("sink did not forward events unchanged: %+v", inner)
	}

	s.Close()
	lines := readLines(t, filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if len(lines) != 2 {
		t.Fatalf("want 2 flywheel lines, got %d", len(lines))
	}
	var first Line
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Schema != "genai.event.v1" || first.Span != "tool" || first.Kind != "tool_use" {
		t.Fatalf("bad line header: %+v", first)
	}
	if first.Tool == nil || first.Tool.Name != "edit_file" || first.Tool.InputDigest == "" {
		t.Fatalf("bad tool line: %+v", first)
	}
	var second Line
	_ = json.Unmarshal(lines[1], &second)
	if second.Err != "permission" || second.Tool == nil || second.Tool.DurationMs != 412 {
		t.Fatalf("error code / duration not captured: %+v", second)
	}
}

func TestSinkSnapshotUsageAndTurn(t *testing.T) {
	dir := t.TempDir()
	s := NewSink(nil, dir, "")
	defer s.Close()

	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 20}
	s.Emit(event.Event{Kind: event.Usage, Usage: u, ModelRef: "gpt-5.5"})
	s.Emit(event.Event{Kind: event.TurnDone, Outcome: "success"})
	s.Close()

	lines := readLines(t, filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	var usage Line
	_ = json.Unmarshal(lines[0], &usage)
	if usage.Kind != "usage" || usage.Model == nil || usage.Model.PromptTokens != 100 {
		t.Fatalf("usage line wrong: %+v", usage)
	}
	var turn Line
	_ = json.Unmarshal(lines[1], &turn)
	if turn.Kind != "turn" || turn.Payload != "success" {
		t.Fatalf("turn line wrong: %+v", turn)
	}
}

func TestSinkIgnoresUncapturedKinds(t *testing.T) {
	dir := t.TempDir()
	s := NewSink(nil, dir, "")
	defer s.Close()
	s.Emit(event.Event{Kind: event.Notice, Text: "notice should not be captured"})
	s.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("uncaptured kinds must not create files, got %v", entries)
	}
}

func TestMCPRecorder(t *testing.T) {
	dir := t.TempDir()
	r := NewMCPRecorder(filepath.Join(dir, "mcp"))
	r.Record("godot-mcp", "godot_scene_snapshot", "scene=main.tscn", "ok nodes=42", 18*time.Millisecond, "")
	r.Record("ocr-mcp", "ocr_image", "path=shot.png", "", 2*time.Second, "timeout")
	r.Close()

	lines := readLines(t, filepath.Join(dir, "mcp", time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if len(lines) != 2 {
		t.Fatalf("want 2 mcp lines, got %d", len(lines))
	}
	var first MCPCallLine
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Schema != "mcp.call.v1" || first.Server != "godot-mcp" || first.DurationMs != 18 || first.ErrorCode != "" {
		t.Fatalf("first mcp line wrong: %+v", first)
	}
	var second MCPCallLine
	_ = json.Unmarshal(lines[1], &second)
	if second.ErrorCode != "timeout" || second.ArgsDigest == "" {
		t.Fatalf("second mcp line wrong: %+v", second)
	}
}

func TestWriterConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			w.Append([]byte(`{"n":` + strings.Repeat("x", 10) + `}`))
		}
	}()
	for i := 0; i < 50; i++ {
		w.Append([]byte(`{"n":` + strings.Repeat("y", 10) + `}`))
	}
	<-done
	w.Close()
	lines := readLines(t, filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if len(lines) != 100 {
		t.Fatalf("concurrent appends lost lines: got %d, want 100", len(lines))
	}
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
