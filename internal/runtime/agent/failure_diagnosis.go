package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// Which lines of a failed command's output carry the diagnosis is a reading
// task, and a model is already reading this transcript to compact it. Asking it
// here costs one small call per protected failure, once: the answer is recorded
// on the message, so every later fold shortens the same way without asking
// again. Word matching stays as the fallback for when this is unavailable.
const (
	diagnosisTimeout      = 45 * time.Second
	diagnosisMaxOutTokens = 256
	// diagnosisMaxInputLines bounds what is offered for selection. Beyond this
	// the numbering itself would cost more than the call can save.
	diagnosisMaxInputLines = 400
)

const diagnosisSystemPrompt = `You are selecting which lines of a failed command's output must be kept verbatim, so a coding agent can act on the failure later without re-running the command.

You will receive the output with each line numbered as "N| text".

Reply with ONLY line numbers and ranges, comma-separated, nothing else. Example:
3,12-15,88

Keep: the assertion or error text, the file:line it points at, stack frames, compiler/linter diagnostics, and the command's final verdict.
Drop: passing cases, progress and discovery noise, repeated identical lines, and anything a re-run would reproduce trivially.
Keep it tight — a few dozen lines at most. If nothing in the output explains the failure, reply with the last few line numbers.`

// annotateFailureDiagnostics records, on each protected failure that has no
// selection yet, which lines a model considers the diagnosis. protected marks
// the retained indexes (nil: all); a folded failure never uses the selection.
// Messages it could not annotate fall back to the mechanical shortening.
func (a *Agent) annotateFailureDiagnostics(ctx context.Context, region []provider.Message, protected []bool) []provider.Message {
	if a == nil || a.svc.prov == nil {
		return region
	}
	out := region
	copied := false
	for i, m := range region {
		if (protected != nil && (i >= len(protected) || !protected[i])) || !wantsDiagnosisSelection(m) {
			continue
		}
		lines, err := a.selectDiagnosticLines(ctx, m.Content)
		if err != nil || len(lines) == 0 {
			continue
		}
		if !copied {
			out = append([]provider.Message(nil), region...)
			copied = true
		}
		ex := *out[i].ToolExecution
		ex.DiagnosticLines = lines
		out[i].ToolExecution = &ex
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// wantsDiagnosisSelection reports whether a message is a protected failure that
// is long enough to be worth a call and has no recorded selection yet.
func wantsDiagnosisSelection(m provider.Message) bool {
	if !failedExecution(m.ToolExecution) || len(m.ToolExecution.DiagnosticLines) > 0 {
		return false
	}
	if strings.Contains(m.Content, elisionMarker) {
		return false
	}
	return strings.Count(m.Content, "\n")+1 >= failureMinLines
}

func (a *Agent) selectDiagnosticLines(ctx context.Context, content string) ([]int, error) {
	lines := strings.Split(content, "\n")
	if len(lines) > diagnosisMaxInputLines {
		return nil, fmt.Errorf("output too long to number (%d lines)", len(lines))
	}
	var numbered strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&numbered, "%d| %s\n", i+1, line)
	}

	ctx, cancel := context.WithTimeout(ctx, diagnosisTimeout)
	defer cancel()
	ctx = provider.WithRequestAttemptCounter(ctx)
	var usage *provider.Usage
	defer func() {
		usage = provider.UsageWithRequestAttemptCount(ctx, usage)
		if usage != nil && (usage.TotalTokens > 0 || usage.RequestCount > 0) {
			a.svc.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage,
				Pricing: a.svc.pricing, UsageSource: event.UsageSourceCompaction})
		}
	}()
	defer TrackPublishedHostStream(ctx, cancel)()

	ch, err := a.svc.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: diagnosisSystemPrompt},
			{Role: provider.RoleUser, Content: numbered.String()},
		},
		MaxTokens:   diagnosisMaxOutTokens,
		Temperature: provider.OptionalTemperature(a.temperature),
	})
	if err != nil {
		return nil, err
	}
	var reply strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			reply.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return nil, chunk.Err
		}
	}
	return parseDiagnosticLines(reply.String(), len(lines)), nil
}

// parseDiagnosticLines reads the model's selection defensively: anything that
// is not a line number in range is dropped rather than trusted, so a chatty or
// miscounted reply degrades to a smaller selection instead of a wrong one.
func parseDiagnosticLines(reply string, total int) []int {
	seen := make(map[int]bool)
	var out []int
	add := func(n int) {
		if n >= 1 && n <= total && !seen[n] && len(out) < failureMaxKeptLines {
			seen[n], out = true, append(out, n)
		}
	}
	for _, field := range strings.FieldsFunc(reply, func(r rune) bool {
		return r != '-' && (r < '0' || r > '9')
	}) {
		lo, hi, isRange := strings.Cut(field, "-")
		start, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			continue
		}
		if !isRange {
			add(start)
			continue
		}
		end, err := strconv.Atoi(strings.TrimSpace(hi))
		if err != nil || end < start || end-start > total {
			add(start)
			continue
		}
		for n := start; n <= end; n++ {
			add(n)
		}
	}
	return out
}

// keepFromSelection renders content down to the recorded lines, reusing the
// elision marker so a selected result is indistinguishable in shape from a
// mechanically shortened one.
func keepFromSelection(content string, selection []int) string {
	lines := strings.Split(content, "\n")
	keep := make([]bool, len(lines))
	kept := 0
	for _, n := range selection {
		if n >= 1 && n <= len(lines) && !keep[n-1] {
			keep[n-1], kept = true, kept+1
		}
	}
	if kept == 0 || kept >= len(lines) {
		return content
	}
	return renderKeptLines(lines, keep)
}
