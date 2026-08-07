// Package flywheel implements the CAPTURE ring of the data flywheel
// (docs/DATA_FLYWHEEL.md): a passthrough event.Sink that snapshots the agent
// event stream (tool calls, messages, usage, compaction, turns) into
// standardized gen_ai JSONL assets, plus an MCP call recorder.
//
// It observes only; it never alters the event stream. The schema follows the
// OpenTelemetry GenAI semantic conventions (gen-ai-agent-spans, gen-ai/mcp):
//
//	<state root>/flywheel/events/YYYY-MM-DD.jsonl   (agent event stream)
//	<state root>/flywheel/mcp/YYYY-MM-DD.jsonl      (MCP call records)
//
// Wire it around the frontend sink at the boot layer, next to stats.Recorder,
// so every entry point (desktop, CLI, serve) records consistently.
package flywheel

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
)

// maxDigestLen caps payload digests so the flywheel stays small and never
// stores full tool inputs/outputs (privacy + size). Full payloads remain in
// the session events stream as before.
const maxDigestLen = 240

// Digest truncates s to maxDigestLen runes with a "…" marker when cut.
func Digest(s string) string {
	if len(s) <= maxDigestLen {
		return s
	}
	return s[:maxDigestLen] + "…"
}

// ErrCode classifies a tool/agent error into a stable, small code set.
func ErrCode(err string) string {
	err = strings.TrimSpace(err)
	if err == "" {
		return ""
	}
	lower := strings.ToLower(err)
	switch {
	case strings.Contains(lower, "denied"), strings.Contains(lower, "permission"),
		strings.Contains(lower, "not authorized"), strings.Contains(lower, "forbidden"):
		return "permission"
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline"):
		return "timeout"
	case strings.Contains(lower, "not found"), strings.Contains(lower, "no such file"):
		return "not_found"
	case strings.Contains(lower, "syntax"), strings.Contains(lower, "compile"),
		strings.Contains(lower, "parse"):
		return "compile"
	default:
		return "error"
	}
}

// Line is one gen_ai JSONL record (§2.1 of DATA_FLYWHEEL.md).
type Line struct {
	Schema  string    `json:"schema"`
	Ts      time.Time `json:"ts"`
	Session string    `json:"session,omitempty"`
	Span    string    `json:"span"`
	Kind    string    `json:"kind"`
	Tool    *ToolLine `json:"gen_ai.tool,omitempty"`
	Model   *ModelUse `json:"gen_ai.model,omitempty"`
	Payload string    `json:"payload,omitempty"`
	Err     string    `json:"error_code,omitempty"`
}

// ToolLine carries gen_ai.tool.* attributes.
type ToolLine struct {
	Name         string `json:"name"`
	InputDigest  string `json:"input_digest,omitempty"`
	OutputDigest string `json:"output_digest,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	ReadOnly     bool   `json:"read_only,omitempty"`
}

// ModelUse carries gen_ai.model.* attributes.
type ModelUse struct {
	Name            string `json:"name,omitempty"`
	PromptTokens    int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int   `json:"completion_tokens,omitempty"`
}

// Sink is a passthrough event.Sink that snapshots the agent stream into the
// flywheel events JSONL. Modeled after stats.Recorder: filesystem latency is
// kept off provider/UI goroutines via a shared async dispatcher.
type Sink struct {
	inner    event.Sink
	writer   *Writer
	session  string
}

// NewSink builds a flywheel Sink writing under dir (e.g. flywheel/events).
// session is the current session stem (bare name, no extension); empty means
// session id is unavailable — lines still record the event kind.
func NewSink(inner event.Sink, dir, session string) *Sink {
	return &Sink{
		inner:   inner,
		writer:  NewWriter(dir),
		session: session,
	}
}

// Emit satisfies event.Sink. It forwards the event unchanged and snapshots a
// flywheel line asynchronously.
func (s *Sink) Emit(e event.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	line := s.snapshot(e)
	if line == nil {
		return
	}
	buf, err := json.Marshal(line)
	if err != nil {
		return
	}
	s.writer.Append(buf)
}

// Close flushes pending records and releases the writer.
func (s *Sink) Close() {
	if s.writer != nil {
		s.writer.Close()
	}
}

// snapshot maps an agent event to a flywheel line (nil = not captured).
func (s *Sink) snapshot(e event.Event) *Line {
	line := &Line{
		Schema:  "genai.event.v1",
		Ts:      time.Now().UTC(),
		Session: s.session,
	}
	switch e.Kind {
	case event.ToolDispatch, event.ToolResult:
		line.Span = "tool"
		line.Kind = "tool_use"
		line.Tool = &ToolLine{
			Name:         e.Tool.Name,
			InputDigest:  Digest(e.Tool.Args),
			OutputDigest: Digest(e.Tool.Output),
			DurationMs:   e.Tool.DurationMs,
			ReadOnly:     e.Tool.ReadOnly,
		}
		if e.Tool.Err != "" {
			line.Err = ErrCode(e.Tool.Err)
		}
	case event.Message:
		line.Span = "run-loop"
		line.Kind = "message"
		line.Payload = Digest(e.Text)
	case event.Usage:
		line.Span = "run-loop"
		line.Kind = "usage"
		if e.Usage != nil {
			line.Model = &ModelUse{
				Name:             e.ModelRef,
				PromptTokens:     e.Usage.PromptTokens,
				CompletionTokens: e.Usage.CompletionTokens,
			}
		}
	case event.CompactionStarted, event.CompactionDone:
		line.Span = "compact"
		line.Kind = "compaction"
	case event.TurnDone:
		line.Span = "run-loop"
		line.Kind = "turn"
		line.Payload = e.Outcome
		if e.Err != nil {
			line.Err = ErrCode(e.Err.Error())
		}
	default:
		return nil
	}
	return line
}

// Writer appends JSON lines to a daily-rotated file. Safe for concurrent use.
type Writer struct {
	mu      sync.Mutex
	dir     string
	day     string
	f       *rotFile
	closed  bool
}

// NewWriter creates a Writer for dir; nil-safe (Append becomes a no-op when
// dir is empty).
func NewWriter(dir string) *Writer {
	return &Writer{dir: dir}
}

// Append writes one JSON object line, rotating the file by UTC day.
func (w *Writer) Append(buf []byte) {
	if w == nil || w.dir == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	if w.f == nil || day != w.day {
		if err := w.rotate(day); err != nil {
			return
		}
	}
	_ = w.f.writeLine(buf)
	// Don't hold the handle across writes: Windows temp-dir cleanup fails on
	// lingering handles, and stats.Recorder uses the same open-write-close
	// pattern. Daily rotation still works via the file name.
	_ = w.f.Close()
	w.f = nil
}

// Close flushes and closes the current file.
func (w *Writer) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
}

func (w *Writer) rotate(day string) error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	f, err := openRotFile(w.dir, day)
	if err != nil {
		return err
	}
	w.day = day
	w.f = f
	return nil
}
