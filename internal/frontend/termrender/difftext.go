package termrender

import (
	"strings"

	"reasonix/internal/contract/event"
)

// DiffText renders a whole unified diff — a shell result running `git diff`, say
// — as a header and highlighted body per file section. A configured
// [cli].diff_formatter takes the whole diff on stdin and re-emits its stdout
// with non-SGR escapes stripped, mirroring the fenced-diff and writer-card
// paths; rows carry the card's body indent and maxLines folds each section.
func DiffText(diff string, width, maxLines int) []string {
	diff = strings.TrimRight(diff, "\n")
	if diff == "" {
		return nil
	}
	if out, ok := formatDiffCached(diff+"\n", max(width-2, 1)); ok {
		return indentRows(strings.Split(strings.TrimRight(out, "\n"), "\n"))
	}
	if hasSGR(diff) {
		return verbatimDiffBody(diff, width, maxLines)
	}
	var rows []string
	for _, sec := range splitDiffSections(diff) {
		body := trimDiffPreamble(sec)
		if body == "" {
			// A section with no "--- "/"+++ " header carries no diff this path
			// can lay out — a `git show` commit header, say — so drop it.
			continue
		}
		path := diffFencePath(body)
		if header := diffFenceHeader(path, countDiff(body)); header != "" {
			rows = append(rows, "  "+header)
		}
		rows = append(rows, diffBody(event.FileDiff{Diff: body}, path, width, maxLines)...)
	}
	return rows
}

func indentRows(lines []string) []string {
	rows := make([]string, 0, len(lines))
	for _, ln := range lines {
		rows = append(rows, "  "+ln)
	}
	return rows
}
