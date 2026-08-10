package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// Keeping a recorded failure whole is what makes a projection grow without
// bound: a failing `go test` log is mostly passing cases, and the keep policy
// protects all of it forever. These shorten one to the lines that carry the
// failure, which is the part a re-run would otherwise be needed to recover.
const (
	failureContextLines = 2  // lines kept either side of a failure line
	failureMaxKeptLines = 60 // ceiling, so a log that fails throughout still shrinks
	failureMinLines     = 24 // below this the result is already small enough to keep whole
)

// elisionMarker labels the gap a previous pass left behind. Its presence means
// the content is already shortened: shortening again would swallow the marker
// and restate the count, so a projection folded twice would differ byte for
// byte from one folded once, for no gain.
const elisionMarker = " lines omitted …"

// failureMarkers mark a line as carrying the failure. Deliberately generous:
// keeping a spare line costs a few tokens, dropping the assertion costs a re-run.
var failureMarkers = []string{
	"fail", "error", "panic:", "fatal", "assert", "expected", "want:", "got:",
	"exit status", "undefined:", "cannot ", "no such", "timeout", "timed out",
}

// keptForProjection shortens a protected failure before it rides into the
// projection. The keep policy exists so the failure is not lost, not so its
// passing noise is carried through every later fold.
func (a *Agent) keptForProjection(m provider.Message) provider.Message {
	if !failedExecution(m.ToolExecution) {
		return m
	}
	m.Content = snipFailureResult(m.Content)
	return m
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
	if strings.Contains(content, elisionMarker) {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) < failureMinLines {
		return content
	}
	keep := make([]bool, len(lines))
	kept := 0
	for i, line := range lines {
		if kept >= failureMaxKeptLines {
			break
		}
		if !isFailureLine(line) {
			continue
		}
		for j := max(0, i-failureContextLines); j <= min(len(lines)-1, i+failureContextLines); j++ {
			if !keep[j] {
				keep[j], kept = true, kept+1
			}
		}
	}
	if kept == 0 || kept >= len(lines) {
		return content
	}

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
