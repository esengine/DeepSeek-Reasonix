package cli

import "strings"

// wrapCache keeps the viewport's wrapped line list in sync with transcript
// blocks without re-wrapping the entire history on every streaming update.
//
// Contract:
//   - wrapWidth is the content width used for the cached lines.
//   - wrapBlockCount is how many leading transcript blocks are fully reflected
//     in wrapBlockLines / the corresponding prefix of wrappedLines.
//   - invalidateWrapFrom(i) drops the cache from block i onward so the next
//     sync only re-wraps the suffix (streaming answer rewrites, tool tails).
//   - Full rebuild is reserved for width change, history shrink, or an empty
//     cache — never for a routine transcriptDirty flag alone.
func (m *chatTUI) clearWrapCache() {
	m.wrappedLines = nil
	m.wrapWidth = 0
	m.wrapBlockCount = 0
	m.wrapBlockLines = nil
}

// syncWrappedLines ensures wrapBlockLines/wrappedLines match m.transcript at
// contentW. forceFull rebuilds every block; otherwise only blocks from
// wrapBlockCount onward are wrapped (suffix path after invalidateWrapFrom).
// Returns true when the flat line list changed and the viewport must be fed.
func (m *chatTUI) syncWrappedLines(contentW int, forceFull bool) bool {
	if contentW <= 0 {
		contentW = 1
	}
	n := len(m.transcript)
	if forceFull || contentW != m.wrapWidth || n < m.wrapBlockCount {
		return m.rebuildWrappedLinesFull(contentW)
	}
	// Heal a desynced block slice (should not happen if invalidate is used).
	if len(m.wrapBlockLines) != m.wrapBlockCount {
		if m.wrapBlockCount > len(m.wrapBlockLines) {
			m.wrapBlockCount = len(m.wrapBlockLines)
		} else {
			m.wrapBlockLines = m.wrapBlockLines[:m.wrapBlockCount]
		}
		m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
	}
	if n == m.wrapBlockCount {
		return false
	}
	// Suffix-only: re-wrap mutated/new blocks from wrapBlockCount..n.
	for i := m.wrapBlockCount; i < n; i++ {
		blockLines := wrapBlockLines(m.transcript[i], contentW)
		m.wrapBlockLines = append(m.wrapBlockLines, blockLines)
	}
	// Rebuild the flat list from the (prefix-stable + new suffix) block wraps
	// into a fresh slice so the viewport never aliases a growing backing array.
	m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
	m.wrapBlockCount = n
	m.wrapWidth = contentW
	return true
}

func (m *chatTUI) rebuildWrappedLinesFull(contentW int) bool {
	if contentW <= 0 {
		contentW = 1
	}
	n := len(m.transcript)
	m.wrapBlockLines = make([][]string, n)
	for i := range n {
		m.wrapBlockLines[i] = wrapBlockLines(m.transcript[i], contentW)
	}
	// Prefer per-block flatten over join-then-wrap so streaming suffix rebuilds
	// stay consistent with append-only updates (both use wrapBlockLines).
	m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
	m.wrapBlockCount = n
	m.wrapWidth = contentW
	return true
}

// feedViewportContent pushes the wrap cache into the bubbles viewport without
// re-joining the document to a single string. The line slice is cloned so later
// cache growth cannot mutate storage the viewport still holds.
func (m *chatTUI) feedViewportContent() {
	if len(m.wrappedLines) == 0 {
		m.viewport.SetContentLines(nil)
		return
	}
	lines := make([]string, len(m.wrappedLines))
	copy(lines, m.wrappedLines)
	m.viewport.SetContentLines(lines)
}

// wrapBlockLines wraps one transcript block to width as a line slice.
func wrapBlockLines(block string, width int) []string {
	wrapped := wrapTranscript(block, width)
	if wrapped == "" {
		return []string{""}
	}
	return strings.Split(wrapped, "\n")
}

// flattenBlockWraps concatenates per-block wrapped line groups. Block boundaries
// in the transcript are already forced newlines (strings.Join(blocks, "\n")), so
// concatenating independent wraps matches the joined document for our content.
func flattenBlockWraps(blocks [][]string) []string {
	if len(blocks) == 0 {
		return nil
	}
	n := 0
	for _, b := range blocks {
		n += len(b)
	}
	out := make([]string, 0, n)
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

// wrappedContentString returns the viewport payload for tests/debug.
func (m chatTUI) wrappedContentString() string {
	if len(m.wrappedLines) == 0 {
		return ""
	}
	return strings.Join(m.wrappedLines, "\n")
}

// invalidateWrapFrom drops the wrap cache from block index onward so the next
// syncWrappedLines only re-wraps the suffix. Used by setTranscriptBlock and any
// in-place rewrite of a live stream slot (answer, tool tail, …).
func (m *chatTUI) invalidateWrapFrom(index int) {
	if index < 0 {
		index = 0
	}
	if index >= m.wrapBlockCount {
		return
	}
	m.wrapBlockLines = m.wrapBlockLines[:index]
	m.wrapBlockCount = index
	m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
}

// rewriteTranscriptBlock updates a block's rendered text and invalidates the
// wrap suffix from that index. Prefer this over bare transcript[i] = … so the
// streaming hot path never forces a full-history rebuild.
func (m *chatTUI) rewriteTranscriptBlock(index int, rendered string) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.transcript[index] = rendered
	m.invalidateWrapFrom(index)
}
