package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// Keeping a recorded failure whole is what makes a projection grow without
// bound: a failing `go test` log is mostly passing cases, and the keep policy
// protects all of it forever. These shorten one to the lines that carry the
// failure, which is the part a re-run would otherwise be needed to recover.
const (
	failureContextLines = 2  // lines kept either side of a failure line
	failureMaxKeptLines = 60 // ceiling, so a log that fails throughout still shrinks
	failureMinLines     = 24 // below this the result is already small enough to keep whole
	// failureTailLines is the position floor, kept whether or not any word in it
	// is recognised: a command's verdict lands at the end — the FAIL summary,
	// the error count, the panic, the last thing printed before a non-zero
	// exit. Word matching alone drops that whenever some unrelated line happens
	// to carry a listed word, because one stray hit is what switches snipping on.
	failureTailLines = 12
	failureHeadLines = 3 // the command and its first context, for orientation
)

// elisionMarker labels the gap a previous pass left behind. Its presence means
// the content is already shortened: shortening again would swallow the marker
// and restate the count, so a projection folded twice would differ byte for
// byte from one folded once, for no gain.
const elisionMarker = " lines omitted …"

// failureMarkers find failure detail buried mid-log, above the tail the
// position floor already keeps. Deliberately generous: keeping a spare line
// costs a few tokens, dropping the assertion costs a re-run. They are a
// supplement, never the only thing standing between a failure and the elision.
var failureMarkers = []string{
	"fail", "error", "panic:", "fatal", "assert", "expected", "want:", "got:",
	"exit status", "undefined:", "cannot ", "no such", "timeout", "timed out",
}

// recordedTailLines is how many trailing lines the runner itself captured as
// the failure diagnosis (ToolExecution.OutputTail, populated only on failure).
// Following it beats a fixed guess: the tail is recorded by the code that saw
// the process exit. Runs without one — a non-shell tool, or a command that
// exited zero but failed verification — keep the fixed floor.
func recordedTailLines(ex *provider.ToolExecution) int {
	if ex == nil {
		return failureTailLines
	}
	tail := strings.TrimRight(ex.OutputTail, "\n")
	if tail == "" {
		return failureTailLines
	}
	return min(max(failureTailLines, strings.Count(tail, "\n")+1), failureMaxKeptLines)
}

func isFailureLine(s string) bool {
	lowered := strings.ToLower(s)
	for _, marker := range failureMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// snipFailureResult keeps the failure-carrying lines of content and elides the
// rest. It returns content unchanged when there is nothing worth dropping, so
// an unchanged return means "leave this message alone".
func snipFailureResult(content string) string {
	return snipFailureTail(content, failureTailLines)
}

func snipFailureTail(content string, tailLines int) string {
	if strings.Contains(content, elisionMarker) {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) < failureMinLines {
		return content
	}
	keep := make([]bool, len(lines))
	kept := 0
	mark := func(i int) {
		if i >= 0 && i < len(lines) && !keep[i] {
			keep[i], kept = true, kept+1
		}
	}
	// The position floor comes first and ignores the ceiling: it is the part
	// that must survive even when word matching finds nothing here, or finds
	// only an unrelated line elsewhere.
	for i := range min(failureHeadLines, len(lines)) {
		mark(i)
	}
	for i := max(0, len(lines)-tailLines); i < len(lines); i++ {
		mark(i)
	}
	for i, line := range lines {
		if kept >= failureMaxKeptLines {
			break
		}
		if !isFailureLine(line) {
			continue
		}
		for j := max(0, i-failureContextLines); j <= min(len(lines)-1, i+failureContextLines); j++ {
			mark(j)
		}
	}
	if kept == 0 || kept >= len(lines) {
		return content
	}
	return renderKeptLines(lines, keep)
}

// renderKeptLines joins the kept lines and labels each gap with what it cost,
// so a reader can tell content was dropped rather than never produced.
func renderKeptLines(lines []string, keep []bool) string {
	var b strings.Builder
	omitted := 0
	flush := func() {
		if omitted > 0 {
			fmt.Fprintf(&b, "… %d%s\n", omitted, elisionMarker)
			omitted = 0
		}
	}
	for i, line := range lines {
		if !keep[i] {
			omitted++
			continue
		}
		flush()
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return strings.TrimRight(b.String(), "\n")
}
