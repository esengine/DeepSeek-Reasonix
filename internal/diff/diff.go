// Package diff computes a line-level diff between two versions of a file and
// renders it as a unified diff, with added/removed line counts. It is a pure,
// dependency-free leaf package: the writer tools use it to preview a pending
// change (the new content a write_file / edit_file / multi_edit would produce)
// without touching disk, so a front-end can show an approval card or a
// changed-files panel before the call runs.
//
// The line diff uses Myers' O(ND) algorithm — the same shortest-edit-script
// approach git uses — so the hunks read the way a developer expects rather than
// a naive longest-common-subsequence walk.
package diff

import "strings"

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
}

// defaultContext is how many unchanged lines surround each change in the
// unified diff, matching the conventional `diff -u` default.
const defaultContext = 3

// Build computes the Change from old to new text for path. kind is supplied by
// the caller (it knows whether the file existed); Build fills the diff and the
// line tallies. When either side contains a NUL byte the file is treated as
// binary: the tallies are left zero and Diff is empty.
func Build(path, oldText, newText string, kind Kind) Change {
	c := Change{Path: path, Kind: kind, OldText: oldText, NewText: newText}
	if isBinary(oldText) || isBinary(newText) {
		c.Binary = true
		return c
	}
	if oldText == newText {
		return c // no-op change; empty diff, zero tallies
	}

	oldLines, oldEOL := splitLines(oldText)
	newLines, newEOL := splitLines(newText)
	ops, ok := myers(oldLines, newLines)
	if !ok {
		// Change too large for an O(N²) line diff (a big rewrite). Give cheap,
		// order-insensitive tallies and omit the unreadable diff.
		c.Added, c.Removed = approxTally(oldLines, newLines)
		c.Diff = "(diff omitted: change too large to render — +" + itoa(c.Added) + " / -" + itoa(c.Removed) + " lines)"
		return c
	}

	for _, op := range ops {
		switch op.typ {
		case opInsert:
			c.Added++
		case opDelete:
			c.Removed++
		}
	}
	c.Diff = unified(path, ops, oldEOL, newEOL, defaultContext)
	return c
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
// newline does not produce a spurious empty final line; its absence is recorded
// so the unified renderer can emit the "\ No newline at end of file" marker.
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

// --- Myers O(ND) shortest-edit-script line diff ---

type opType int

const (
	opEqual opType = iota
	opDelete
	opInsert
)

// op is one line of the edit script: a line kept, removed, or added.
type op struct {
	typ  opType
	line string
}

// maxDiffEdits caps the edit distance Myers will explore. The trace it records is
// O(D) snapshots of an O(D)-wide vector, so an unbounded D (a full rewrite of a
// large file) is O(D²) ≈ O(N²) time and memory — gigabytes for a few-thousand-line
// rewrite, which would OOM the synchronous preview. Small edits converge well
// below this regardless of file size; a change that exceeds it is a near-total
// rewrite whose line-by-line diff is unreadable anyway, so we fall back to tallies.
const maxDiffEdits = 2000

// myers returns the shortest edit script transforming a into b, line by line, and
// ok=true. It records the search trace, then backtracks it into an ordered op
// list. ok is false when the edit distance exceeds maxDiffEdits — the caller then
// skips the rendered diff rather than pay O(N²).
func myers(a, b []string) ([]op, bool) {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil, true
	}
	maxD := n + m
	if maxD > maxDiffEdits || maxD < 0 {
		maxD = maxDiffEdits // bound the trace's O(D²) footprint (and any n+m overflow)
	}
	offset := maxD // shift negative k into a non-negative array index
	v := make([]int, 2*maxD+1)
	var trace [][]int

	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			// Pick the move that reaches furthest: down (insert from b) when at
			// the lower edge or the line below is ahead, else right (delete a).
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] { // slide down the diagonal
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, offset), true
			}
		}
	}
	return nil, false // edit distance exceeded maxDiffEdits — caller falls back
}

// backtrack walks the recorded trace from the end back to the origin,
// reconstructing the diagonal (equal), down (insert), and right (delete) moves,
// then reverses them into forward order.
func backtrack(trace [][]int, a, b []string, offset int) []op {
	x, y := len(a), len(b)
	var ops []op
	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1 // came from a down move (insert)
		} else {
			prevK = k - 1 // came from a right move (delete)
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY { // diagonal: equal lines
			ops = append(ops, op{opEqual, a[x-1]})
			x--
			y--
		}
		if x == prevX {
			ops = append(ops, op{opInsert, b[y-1]})
		} else {
			ops = append(ops, op{opDelete, a[x-1]})
		}
		x, y = prevX, prevY
	}
	// d == 0 leg: any remaining lines are a common prefix (all equal).
	for x > 0 && y > 0 {
		ops = append(ops, op{opEqual, a[x-1]})
		x--
		y--
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

// --- unified-diff rendering ---

// lineRef pairs an op with the 1-based line numbers it occupies in the old and
// new files, so the hunk header counts are exact.
type lineRef struct {
	op           op
	oldNo, newNo int // 0 when the line is absent on that side
}

// unified renders ops as a unified diff with the given context. It returns ""
// when there is nothing to show. oldEOL/newEOL report whether each side ended
// with a newline, used to emit the "\ No newline at end of file" marker.
func unified(path string, ops []op, oldEOL, newEOL bool, context int) string {
	refs := number(ops)
	hunks := group(refs, context)
	if len(hunks) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")

	lastOldNo, lastNewNo := lastLineNumbers(refs)
	for _, h := range hunks {
		writeHunkHeader(&b, refs, h)
		for i := h.start; i < h.end; i++ {
			r := refs[i]
			switch r.op.typ {
			case opEqual:
				b.WriteString(" " + r.op.line + "\n")
			case opDelete:
				b.WriteString("-" + r.op.line + "\n")
				if !oldEOL && r.oldNo == lastOldNo {
					b.WriteString("\\ No newline at end of file\n")
				}
			case opInsert:
				b.WriteString("+" + r.op.line + "\n")
				if !newEOL && r.newNo == lastNewNo {
					b.WriteString("\\ No newline at end of file\n")
				}
			}
		}
	}
	return b.String()
}

// number assigns each op its 1-based old/new line numbers.
func number(ops []op) []lineRef {
	refs := make([]lineRef, len(ops))
	oldNo, newNo := 0, 0
	for i, o := range ops {
		r := lineRef{op: o}
		switch o.typ {
		case opEqual:
			oldNo++
			newNo++
			r.oldNo, r.newNo = oldNo, newNo
		case opDelete:
			oldNo++
			r.oldNo = oldNo
		case opInsert:
			newNo++
			r.newNo = newNo
		}
		refs[i] = r
	}
	return refs
}

// lastLineNumbers returns the highest old and new line numbers seen, used to
// decide which removed/added line is the file's final one (for the no-newline
// marker).
func lastLineNumbers(refs []lineRef) (lastOld, lastNew int) {
	for _, r := range refs {
		if r.oldNo > lastOld {
			lastOld = r.oldNo
		}
		if r.newNo > lastNew {
			lastNew = r.newNo
		}
	}
	return lastOld, lastNew
}

// hunk is a half-open range [start,end) of refs emitted together.
type hunk struct{ start, end int }

// group collects change regions, padding each with up to context equal lines on
// both sides and merging regions whose padding overlaps, mirroring `diff -u`.
func group(refs []lineRef, context int) []hunk {
	var changes []int
	for i, r := range refs {
		if r.op.typ != opEqual {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return nil
	}

	var hunks []hunk
	start := max(0, changes[0]-context)
	end := min(len(refs), changes[0]+context+1)
	for _, ci := range changes[1:] {
		// If the next change's leading context reaches the current hunk, extend
		// it; otherwise close this hunk and open a new one.
		if ci-context <= end {
			end = min(len(refs), ci+context+1)
			continue
		}
		hunks = append(hunks, hunk{start, end})
		start = ci - context
		end = min(len(refs), ci+context+1)
	}
	hunks = append(hunks, hunk{start, end})
	return hunks
}

// writeHunkHeader emits the "@@ -oldStart,oldCount +newStart,newCount @@" line
// for a hunk, deriving the ranges from the refs it spans.
func writeHunkHeader(b *strings.Builder, refs []lineRef, h hunk) {
	oldStart, oldCount, newStart, newCount := 0, 0, 0, 0
	for i := h.start; i < h.end; i++ {
		r := refs[i]
		if r.oldNo != 0 {
			if oldStart == 0 {
				oldStart = r.oldNo
			}
			oldCount++
		}
		if r.newNo != 0 {
			if newStart == 0 {
				newStart = r.newNo
			}
			newCount++
		}
	}
	// A side with zero lines in the hunk is conventionally anchored at the line
	// it follows, with start 0 only when the file is empty on that side.
	if oldCount == 0 {
		oldStart = 0
	}
	if newCount == 0 {
		newStart = 0
	}
	b.WriteString("@@ -")
	b.WriteString(rangeSpec(oldStart, oldCount))
	b.WriteString(" +")
	b.WriteString(rangeSpec(newStart, newCount))
	b.WriteString(" @@\n")
}

// rangeSpec formats one side of a hunk header: "start,count", or just "start"
// when count is 1, matching unified-diff convention.
func rangeSpec(start, count int) string {
	if count == 1 {
		return itoa(start)
	}
	return itoa(start) + "," + itoa(count)
}

// itoa is a tiny non-allocating-path integer formatter kept local so the
// package imports only strings.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
