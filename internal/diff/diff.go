// Package diff computes a line-level diff between two versions of a file and
// renders it as a unified diff, with added/removed line counts. It is a pure
// leaf package: the writer tools use it to preview a pending change (the new
// content a write_file / edit_file / multi_edit would produce) without touching
// disk, so a front-end can show an approval card or a changed-files panel before
// the call runs.
package diff

import (
	"strconv"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// Kind classifies what a change does to a file's existence, so a UI can label
// it ("new file", "modified", "deleted") without diffing to find out.
type Kind string

const (
	// Create is a write to a path that did not previously exist.
	Create Kind = "create"
	// Modify edits or overwrites an existing file.
	Modify Kind = "modify"
	// Delete empties an existing file to nothing.
	Delete Kind = "delete"
)

// Change is a previewed (not-yet-applied) edit to one file: the before/after
// text, the unified diff between them, and the line tallies a UI shows as
// "+N / -M". Binary is set when either side looks non-textual, in which case
// Diff is left empty (a byte-diff would be noise).
type Change struct {
	Path    string `json:"path"`
	Kind    Kind   `json:"kind"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
	Added   int    `json:"added"`   // lines present in new but not old
	Removed int    `json:"removed"` // lines present in old but not new
	Diff    string `json:"diff"`    // unified diff; "" when Binary
	Binary  bool   `json:"binary"`
	Mode    string `json:"mode,omitempty"`  // optional render mode metadata
	Hunks   int    `json:"hunks,omitempty"` // unified diff hunk count when rendered
}

type OutputMode string

const (
	OutputModePatch   OutputMode = "patch"
	OutputModePreview OutputMode = "preview"
)

type BuildOptions struct {
	ContextLines int
	OldLabel     string
	NewLabel     string
	Mode         OutputMode
}

// defaultContext is how many unchanged lines surround each change in the
// unified diff, matching the conventional `diff -u` default.
const defaultContext = 3

// maxDiffEdits caps the changed window we will send to an exact line diff. A
// near-total rewrite of a large file is unreadable and can make LCS/Myers-style
// algorithms allocate heavily, so we fall back to tallies before calling the
// third-party renderer.
const maxDiffEdits = 2000

// Build computes the Change from old to new text for path. kind is supplied by
// the caller (it knows whether the file existed); Build fills the diff and the
// line tallies. When either side contains a NUL byte the file is treated as
// binary: the tallies are left zero and Diff is empty.
func Build(path, oldText, newText string, kind Kind) Change {
	return BuildWithOptions(path, oldText, newText, kind, BuildOptions{ContextLines: -1})
}

// BuildWithOptions computes a Change like Build, with explicit render options
// for callers that need preview-style labels or non-default context lines.
func BuildWithOptions(path, oldText, newText string, kind Kind, opts BuildOptions) Change {
	opts = normalizeBuildOptions(path, opts)
	c := Change{Path: path, Kind: kind, OldText: oldText, NewText: newText}
	if opts.Mode != "" && opts.Mode != OutputModePatch {
		c.Mode = string(opts.Mode)
	}
	if isBinary(oldText) || isBinary(newText) {
		c.Binary = true
		return c
	}
	if oldText == newText {
		return c // no-op change; empty diff, zero tallies
	}

	oldLines, _ := splitLines(oldText)
	newLines, _ := splitLines(newText)
	if exactDiffTooLarge(oldLines, newLines) {
		c.Added, c.Removed = approxTally(oldLines, newLines)
		c.Diff = "(diff omitted: change too large to render — +" + itoa(c.Added) + " / -" + itoa(c.Removed) + " lines)"
		return c
	}

	edits := udiff.Lines(oldText, newText)
	c.Added, c.Removed = tallyEdits(oldText, edits)
	diff, err := udiff.ToUnified(opts.OldLabel, opts.NewLabel, oldText, edits, opts.ContextLines)
	if err != nil {
		c.Added, c.Removed = approxTally(oldLines, newLines)
		c.Diff = "(diff omitted: failed to render — +" + itoa(c.Added) + " / -" + itoa(c.Removed) + " lines)"
		return c
	}
	c.Diff = diff
	c.Hunks = countUnifiedHunks(diff)
	return c
}

func normalizeBuildOptions(path string, opts BuildOptions) BuildOptions {
	if opts.ContextLines < 0 {
		opts.ContextLines = defaultContext
	}
	if opts.Mode == "" {
		opts.Mode = OutputModePatch
	}
	if opts.OldLabel == "" {
		prefix := "a/"
		if opts.Mode == OutputModePreview {
			prefix = "before/"
		}
		opts.OldLabel = prefix + path
	}
	if opts.NewLabel == "" {
		prefix := "b/"
		if opts.Mode == OutputModePreview {
			prefix = "after/"
		}
		opts.NewLabel = prefix + path
	}
	return opts
}

func countUnifiedHunks(diff string) int {
	return strings.Count(diff, "\n@@ ")
}

func exactDiffTooLarge(oldLines, newLines []string) bool {
	return changedWindowSize(oldLines, newLines) > maxDiffEdits
}

func changedWindowSize(oldLines, newLines []string) int {
	start := 0
	for start < len(oldLines) && start < len(newLines) && oldLines[start] == newLines[start] {
		start++
	}
	oldEnd := len(oldLines)
	newEnd := len(newLines)
	for oldEnd > start && newEnd > start && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	return (oldEnd - start) + (newEnd - start)
}

func tallyEdits(oldText string, edits []udiff.Edit) (added, removed int) {
	for _, edit := range edits {
		if edit.Start >= 0 && edit.End >= edit.Start && edit.End <= len(oldText) {
			removed += logicalLineCount(oldText[edit.Start:edit.End])
		}
		added += logicalLineCount(edit.New)
	}
	return added, removed
}

func logicalLineCount(s string) int {
	lines, _ := splitLines(s)
	return len(lines)
}

// approxTally counts added/removed lines by multiset difference — order-
// insensitive but O(n+m), used when the exact diff is skipped for being too large.
func approxTally(oldLines, newLines []string) (added, removed int) {
	counts := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		counts[l]++
	}
	for _, l := range newLines {
		if counts[l] > 0 {
			counts[l]--
		} else {
			added++
		}
	}
	for _, c := range counts {
		removed += c
	}
	return added, removed
}

// isBinary reports whether s looks non-textual. A NUL byte never appears in
// UTF-8 text, so it is a cheap, reliable signal — the same heuristic git uses.
func isBinary(s string) bool { return strings.IndexByte(s, 0) >= 0 }

// splitLines breaks s into lines without their terminators and reports whether
// the text ended with a newline. An empty string yields no lines. A trailing
// newline does not produce a spurious empty final line.
func splitLines(s string) (lines []string, endsWithNewline bool) {
	if s == "" {
		return nil, true // vacuously: no missing-newline marker for empty content
	}
	endsWithNewline = strings.HasSuffix(s, "\n")
	if endsWithNewline {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), endsWithNewline
}

func itoa(n int) string { return strconv.Itoa(n) }
