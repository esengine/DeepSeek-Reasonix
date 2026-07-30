package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TextSink renders a turn's event stream to ANSI text on an io.Writer. It is
// the reference terminal frontend: a headless `reasonix run` writes to stdout,
// and during the cache-first migration the chat TUI is fed through it too. The
// output is byte-for-byte what the agent used to print directly, now driven by
// typed events instead of inline Fprint calls.
//
// renderer, when non-nil, replaces the streamed raw answer text with styled
// markdown once the text stream completes (a Message event). termWidth is the
// column count used to count how many rows the raw stream occupied before the
// redraw moves the cursor back. A nil renderer keeps the raw stream — correct
// for piped output and for the chat TUI, which renders markdown itself.
type TextSink struct {
	out       io.Writer
	renderer  Renderer
	termWidth int

	// Per-stream state, reset on Message / TurnStarted.
	wroteReasoningHeader bool
	wroteReasoningBody   bool
	textWritten          bool
	showReasoning        bool
	// Per-turn state, reset on TurnStarted. Tracks whether anything has been
	// written this turn so a coordinator Phase marker leads with a blank line
	// only when it follows earlier output.
	wroteAnything bool
}

// NewTextSink builds a TextSink writing to out. renderer/termWidth drive the
// post-stream markdown redraw; pass a nil renderer to keep the raw stream.
func NewTextSink(out io.Writer, renderer Renderer, termWidth int) *TextSink {
	return &TextSink{out: out, renderer: renderer, termWidth: termWidth}
}

// SetShowReasoning toggles Claude Code-style verbose display for thinking-mode
// reasoning. Reasoning is still kept in session state by the agent; this only
// controls terminal rendering.
func (s *TextSink) SetShowReasoning(show bool) { s.showReasoning = show }

// Emit renders one event. Called serially by the run loop.
func (s *TextSink) Emit(e event.Event) {
	switch e.Kind {
	case event.TurnStarted:
		s.wroteReasoningHeader = false
		s.wroteReasoningBody = false
		s.textWritten = false
		s.wroteAnything = false

	case event.Reasoning:
		if !s.wroteReasoningHeader {
			fmt.Fprintln(s.out, dimText("  ▎ thinking"))
			s.wroteReasoningHeader = true
		}
		if s.showReasoning && e.Text != "" {
			fmt.Fprint(s.out, dimText(e.Text))
			s.wroteReasoningBody = true
		}
		s.wroteAnything = true

	case event.Text:
		if s.wroteReasoningHeader && s.wroteReasoningBody && !s.textWritten {
			fmt.Fprintln(s.out) // separate the reasoning block from the answer
		}
		fmt.Fprint(s.out, e.Text)
		s.textWritten = true
		s.wroteAnything = true

	case event.Message:
		s.closeTextStream(e.Text, e.Reasoning)

	case event.ToolDispatch:
		// The early (Partial) dispatch carries no args — the full one prints the
		// line. A same-ID preview refresh is for upsert-capable frontends; this
		// append-only stream ignores it so every tool still prints exactly once.
		if e.Tool.Partial || e.Tool.Refreshed {
			break
		}
		fmt.Fprintf(s.out, "  -> %s\n", textSinkToolHead(e.Tool.Name, e.Tool.Args))
		s.wroteAnything = true

	case event.ToolResult:
		// A successful result is silent (it only feeds the model); a blocked
		// call surfaces the same "⊘ name <reason>" line the agent used to print.
		if e.Tool.Err != "" {
			name := e.Tool.Name
			if e.Tool.Name == "use_capability" {
				name = textSinkToolHead(e.Tool.Name, e.Tool.Args)
			}
			fmt.Fprintf(s.out, "  ⊘ %s %s\n", name, e.Tool.Err)
			s.wroteAnything = true
		}

	case event.Usage:
		// Close a still-open raw text block before the usage line, matching the
		// old Fprintln path for streams that do not emit a Message redraw.
		if s.textWritten {
			fmt.Fprintln(s.out)
			s.textWritten = false
		}
		s.usageLine(e.Usage, e.Pricing, e.CacheDiagnostics)

	case event.Notice:
		glyph := "·"
		if e.Level == event.LevelWarn {
			glyph = "!"
		}
		fmt.Fprintf(s.out, "  %s %s\n", glyph, e.Text)
		s.wroteAnything = true

	case event.Phase:
		if s.wroteAnything {
			fmt.Fprintln(s.out)
		}
		fmt.Fprintf(s.out, "[%s]\n", e.Text)
		s.wroteAnything = true

	case event.CompactionStarted:
		fmt.Fprintln(s.out, dimText("  ⋯ compacting conversation…"))
		s.wroteAnything = true

	case event.CompactionDone:
		c := e.Compaction
		if c.Summary == "" {
			break // aborted pass — the caller's Notice already explained why
		}
		fmt.Fprintln(s.out, dimText(fmt.Sprintf("  ⋯ compacted %d messages (%s)", c.Messages, c.Trigger)))
		for _, ln := range strings.Split(strings.TrimRight(c.Summary, "\n"), "\n") {
			fmt.Fprintln(s.out, dimText("    "+ln))
		}
		s.wroteAnything = true
	}
}

func textSinkToolHead(name, args string) string {
	if name != "use_capability" {
		return name + " " + CompactArgs(args)
	}
	var call struct {
		Action       string `json:"action"`
		CapabilityID string `json:"capability_id"`
	}
	if json.Unmarshal([]byte(args), &call) != nil {
		return "MCP"
	}
	subject := strings.TrimSpace(call.CapabilityID)
	if subject == "" {
		subject = strings.TrimSpace(call.Action)
	}
	if subject == "" {
		return "MCP"
	}
	return "MCP(" + subject + ")"
}

// closeTextStream ends the streamed answer. With a renderer wired in and the
// stream short enough to scroll back over, it moves the cursor to where text
// began, clears to end of screen, and re-emits the styled markdown; otherwise
// it just terminates the block with a newline. Reasoning above the text is left
// untouched. Mirrors the old Agent.stream tail exactly.
func (s *TextSink) closeTextStream(text, reasoning string) {
	defer func() {
		s.wroteReasoningHeader = false
		s.wroteReasoningBody = false
		s.textWritten = false
	}()
	if len(text) > 0 {
		s.wroteAnything = true
	}
	if len(text) > 0 && s.renderer != nil {
		if moved := streamedRows(text, s.termWidth); moved < 200 {
			if moved == 0 {
				fmt.Fprint(s.out, "\r\033[0J")
			} else {
				fmt.Fprintf(s.out, "\r\033[%dA\033[0J", moved)
			}
			fmt.Fprint(s.out, s.renderer.Render(text))
			return
		}
	}
	if len(text) > 0 || (len(reasoning) > 0 && s.wroteReasoningBody) {
		fmt.Fprintln(s.out)
	}
}

// usageLine writes the one-line token/cache summary; no-op when usage is unset.
func (s *TextSink) usageLine(u *provider.Usage, p *provider.Pricing, d *event.CacheDiagnostics) {
	if line := FormatUsageLine(u, p, d); line != "" {
		fmt.Fprintln(s.out, line)
		s.wroteAnything = true
	}
}

// FormatUsageLine renders the per-turn token/cache summary — the key signal for
// the cache-first design — as a single line (no trailing newline), or "" when
// usage is unset or empty. Cache is reported as absolute "(N cached / M new)"
// so a turn that adds a lot of fresh content doesn't read as "cache broke" the
// way a falling percentage would; the cached prefix is still hitting, the
// denominator just grew. Reasoning tokens (a subset of completion) show the
// chain-of-thought cost. Shared by TextSink and the chat TUI so both frontends
// render the line identically.
func FormatUsageLine(u *provider.Usage, p *provider.Pricing, d *event.CacheDiagnostics) string {
	if u == nil || u.TotalTokens == 0 {
		return ""
	}
	cacheCol := ""
	if u.PromptTokens > 0 {
		cached := u.CacheHitTokens
		fresh := u.CacheMissTokens
		if fresh == 0 {
			if d := u.PromptTokens - cached; d > 0 {
				fresh = d
			}
		}
		cacheCol = fmt.Sprintf(" (%d cached / %d new)", cached, fresh)
	}
	reasoning := ""
	if u.ReasoningTokens > 0 {
		reasoning = fmt.Sprintf(" (%d reasoning)", u.ReasoningTokens)
	}
	cost := ""
	if p != nil {
		cost = fmt.Sprintf(" · %s%.4f", p.Symbol(), p.Cost(u))
	}
	churn := ""
	if d != nil && d.PrefixChanged {
		reasons := strings.Join(d.PrefixChangeReasons, "+")
		if reasons == "" {
			reasons = "unknown"
		}
		churn = fmt.Sprintf(" · cache prefix changed: %s", reasons)
	}
	return fmt.Sprintf("  · %d tok · in %d%s · out %d%s%s%s",
		u.TotalTokens, u.PromptTokens, cacheCol, u.CompletionTokens, reasoning, cost, churn)
}

// dimText wraps s in the ANSI dim SGR sequence so reasoning streams visually
// recede from the final answer.
func dimText(s string) string { return "\x1b[2m" + s + "\x1b[0m" }

// CompactArgs trims and caps a tool's raw JSON arguments for the dispatch line.
// Exported so the CLI can reuse the same rendering without duplicating the logic.
func CompactArgs(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:120]) + "..."
	}
	return s
}
