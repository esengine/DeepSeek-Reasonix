// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package udiff

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

// DefaultContextLines is the number of unchanged lines of surrounding
// context displayed by Unified. Use ToUnified to specify a different value.
const DefaultContextLines = 3

// Unified returns a unified diff of the old and new strings.
// The old and new labels are the names of the old and new files.
// If the strings are equal, it returns the empty string.
func Unified(oldLabel, newLabel, old, new string) string {
	edits := Lines(old, new)
	unified, err := ToUnified(oldLabel, newLabel, old, edits, DefaultContextLines)
	if err != nil {
		// Can't happen: edits are consistent.
		log.Fatalf("internal error in diff.Unified: %v", err)
	}
	return unified
}

// ToUnified applies the edits to content and returns a unified diff,
// with contextLines lines of (unchanged) context around each diff hunk.
// The old and new labels are the names of the content and result files.
// It returns an error if the edits are inconsistent; see ApplyEdits.
func ToUnified(oldLabel, newLabel, content string, edits []Edit, contextLines int) (string, error) {
	u, err := toUnified(oldLabel, newLabel, content, edits, contextLines)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// unified represents a set of edits as a unified diff.
type unified struct {
	// From is the name of the original file.
	From string
	// To is the name of the modified file.
	To string
	// Hunks is the set of edit Hunks needed to transform the file content.
	Hunks []*hunk
}

// Hunk represents a contiguous set of line edits to apply.
type hunk struct {
	// The line in the original source where the hunk starts.
	FromLine int
	// The line in the original source where the hunk finishes.
	ToLine int
	// The set of line based edits to apply.
	Lines []line
}

// Line represents a single line operation to apply as part of a Hunk.
type line struct {
	// Kind is the type of line this represents, deletion, insertion or copy.
	Kind OpKind
	// Content is the Content of this line.
	// For deletion it is the line being removed, for all others it is the line
	// to put in the output.
	Content string
}

// OpKind is used to denote the type of operation a line represents.
type OpKind int

const (
	// Delete is the operation kind for a line that is present in the input
	// but not in the output.
	Delete OpKind = iota
	// Insert is the operation kind for a line that is new in the output.
	Insert
	// Equal is the operation kind for a line that is the same in the input and
	// output, often used to provide context around edited lines.
	Equal
)

// String returns a human readable representation of an OpKind. It is not
// intended for machine processing.
func (k OpKind) String() string {
	switch k {
	case Delete:
		return "delete"
	case Insert:
		return "insert"
	case Equal:
		return "equal"
	default:
		panic("unknown operation kind")
	}
}

// toUnified takes a file contents and a sequence of edits, and calculates
// a unified diff that represents those edits.
func toUnified(fromName, toName string, content string, edits []Edit, contextLines int) (unified, error) {
	gap := contextLines * 2
	u := unified{
		From: fromName,
		To:   toName,
	}
	if len(edits) == 0 {
		return u, nil
	}
	var err error
	edits, err = lineEdits(content, edits) // expand to whole lines
	if err != nil {
		return u, err
	}
	lines, _ := splitLines(content)
	var h *hunk
	last := 0
	toLine := 0
	for _, edit := range edits {
		// Compute the zero-based line numbers of the edit start and end.
		// TODO(adonovan): opt: compute incrementally, avoid O(n^2).
		start := strings.Count(content[:edit.Start], "\n")
		end := strings.Count(content[:edit.End], "\n")
		if edit.End == len(content) && len(content) > 0 && content[len(content)-1] != '\n' {
			end++ // EOF counts as an implicit newline
		}

		switch {
		case h != nil && start == last:
			//direct extension
		case h != nil && start <= last+gap:
			//within range of previous lines, add the joiners
			addEqualLines(h, lines, last, start)
		default:
			//need to start a new hunk
			if h != nil {
				// add the edge to the previous hunk
				addEqualLines(h, lines, last, last+contextLines)
				u.Hunks = append(u.Hunks, h)
			}
			toLine += start - last
			h = &hunk{
				FromLine: start + 1,
				ToLine:   toLine + 1,
			}
			// add the edge to the new hunk
			delta := addEqualLines(h, lines, start-contextLines, start)
			h.FromLine -= delta
			h.ToLine -= delta
		}
		last = start
		for i := start; i < end; i++ {
			h.Lines = append(h.Lines, line{Kind: Delete, Content: lines[i]})
			last++
		}
		if edit.New != "" {
			v, _ := splitLines(edit.New)
			for _, content := range v {
				h.Lines = append(h.Lines, line{Kind: Insert, Content: content})
				toLine++
			}
		}
	}
	if h != nil {
		// add the edge to the final hunk
		addEqualLines(h, lines, last, last+contextLines)
		u.Hunks = append(u.Hunks, h)
	}
	return u, nil
}

// split into lines removing a final empty line,
// and also return the offsets of the line beginnings.
func splitLines(text string) ([]string, []int) {
	var lines []string
	offsets := []int{0}
	start := 0
	for i, r := range text {
		if r == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
			offsets = append(offsets, start)
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
		offsets = append(offsets, len(text))
	}
	return lines, offsets
}

func addEqualLines(h *hunk, lines []string, start, end int) int {
	delta := 0
	for i := start; i < end; i++ {
		if i < 0 {
			continue
		}
		if i >= len(lines) {
			return delta
		}
		h.Lines = append(h.Lines, line{Kind: Equal, Content: lines[i]})
		delta++
	}
	return delta
}

// String converts a unified diff to the standard textual form for that diff.
// The output of this function can be passed to tools like patch.
func (u unified) String() string {
	if len(u.Hunks) == 0 {
		return ""
	}
	b := new(strings.Builder)
	fmt.Fprintf(b, "--- %s\n", u.From)
	fmt.Fprintf(b, "+++ %s\n", u.To)
	for _, hunk := range u.Hunks {
		fromCount, toCount := 0, 0
		for _, l := range hunk.Lines {
			switch l.Kind {
			case Delete:
				fromCount++
			case Insert:
				toCount++
			default:
				fromCount++
				toCount++
			}
		}
		fmt.Fprint(b, "@@")
		if fromCount > 1 {
			fmt.Fprintf(b, " -%d,%d", hunk.FromLine, fromCount)
		} else if hunk.FromLine == 1 && fromCount == 0 {
			// Match odd GNU diff -u behavior adding to empty file.
			fmt.Fprintf(b, " -0,0")
		} else {
			fmt.Fprintf(b, " -%d", hunk.FromLine)
		}
		if toCount > 1 {
			fmt.Fprintf(b, " +%d,%d", hunk.ToLine, toCount)
		} else if hunk.ToLine == 1 && toCount == 0 {
			// Match odd GNU diff -u behavior adding to empty file.
			fmt.Fprintf(b, " +0,0")
		} else {
			fmt.Fprintf(b, " +%d", hunk.ToLine)
		}
		fmt.Fprint(b, " @@\n")
		for _, l := range hunk.Lines {
			switch l.Kind {
			case Delete:
				fmt.Fprintf(b, "-%s", l.Content)
			case Insert:
				fmt.Fprintf(b, "+%s", l.Content)
			default:
				fmt.Fprintf(b, " %s", l.Content)
			}
			if !strings.HasSuffix(l.Content, "\n") {
				fmt.Fprintf(b, "\n\\ No newline at end of file\n")
			}
		}
	}
	return b.String()
}

// ApplyUnified applies the unified diffs.
func ApplyUnified(udiffs, bef string) (string, error) {
	before := strings.Split(bef, "\n")
	unif := strings.Split(udiffs, "\n")
	var got []string
	left := 0
	// parse and apply the unified diffs
	for _, l := range unif {
		if len(l) == 0 {
			continue // probably the last line (from Split)
		}
		switch l[0] {
		case '@': // The @@ line
			m := atregexp.FindStringSubmatch(l)
			fromLine, err := strconv.Atoi(m[1])
			if err != nil {
				return "", fmt.Errorf("missing line number in %q", l)
			}
			// before is a slice, so0-based; fromLine is 1-based
			for ; left < fromLine-1; left++ {
				got = append(got, before[left])
			}
		case '+': // add this line
			if strings.HasPrefix(l, "+++ ") {
				continue
			}
			got = append(got, l[1:])
		case '-': // delete this line
			if strings.HasPrefix(l, "--- ") {
				continue
			}
			left++
		case ' ':
			return "", fmt.Errorf("unexpected line %q", l)
		default:
			return "", fmt.Errorf("impossible unified %q", udiffs)
		}
	}
	// copy any remaining lines
	for ; left < len(before); left++ {
		got = append(got, before[left])
	}
	return strings.Join(got, "\n"), nil
}

// The first number in the @@ lines is the line number in the 'before' data
var atregexp = regexp.MustCompile(`@@ -(\d+).* @@`)
