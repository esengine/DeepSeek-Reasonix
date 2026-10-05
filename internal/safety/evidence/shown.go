package evidence

import "strings"

// shownSampleLimit bounds how many lines a witness carries, so matching costs
// the same on a one-line edit and a rewritten file. A larger change is sampled
// evenly rather than truncated: an output that showed only the beginning must
// not answer for the middle.
const shownSampleLimit = 64

// ChangedLines is what a later output has to show to prove it reviewed a
// change: the lines that differ between the two revisions, added ones first.
// Blank lines are dropped — they occur in every output and distinguish
// nothing — and the result is sampled to shownSampleLimit.
func ChangedLines(before, after string) []string {
	old, now := lineSet(before), lineSet(after)
	witness := make([]string, 0, len(now))
	for _, line := range distinctLines(after) {
		if !old[line] {
			witness = append(witness, line)
		}
	}
	for _, line := range distinctLines(before) {
		if !now[line] {
			witness = append(witness, line)
		}
	}
	return sampleEvenly(witness, shownSampleLimit)
}

// ContentLines is the witness for a file whose earlier revision is unknown — a
// shell write names no before. Showing such a file means showing its content.
func ContentLines(content string) []string {
	return sampleEvenly(distinctLines(content), shownSampleLimit)
}

// OutputShowsLines reports whether every witness line appears in output. The
// test is on substrings by design: `git diff` prefixes its lines with +/-,
// `grep -n` and `cat -n` prefix them with a number, and a rule that had to
// recognize each of those formats would be the command table this replaces.
// An empty witness proves nothing, so it never counts.
func OutputShowsLines(output string, witness []string) bool {
	if len(witness) == 0 || strings.TrimSpace(output) == "" {
		return false
	}
	haystack := strings.ReplaceAll(output, "\r\n", "\n")
	for _, line := range witness {
		if !strings.Contains(haystack, line) {
			return false
		}
	}
	return true
}

func distinctLines(text string) []string {
	seen := map[string]bool{}
	var out []string
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

func lineSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range distinctLines(text) {
		out[line] = true
	}
	return out
}

// sampleEvenly keeps at most limit entries spread across the whole slice.
func sampleEvenly(lines []string, limit int) []string {
	if limit <= 0 || len(lines) <= limit {
		return lines
	}
	out := make([]string, 0, limit)
	for i := range limit {
		out = append(out, lines[i*len(lines)/limit])
	}
	return out
}
