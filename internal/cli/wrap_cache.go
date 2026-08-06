package cli

import "strings"

// wrapCache keeps the viewport's wrapped line list in sync with transcript
// blocks without re-wrapping the entire history on every streaming commit.
//
// Contract:
//   - wrapWidth is the content width used for the cached lines.
//   - wrapBlockCount is how many leading transcript blocks are already reflected.
//   - wrappedLines is the full join of per-block wraps (equivalent to
//     strings.Split(wrapTranscript(strings.Join(transcript, "\n"), width), "\n")
//     because block boundaries are forced newlines).
//
// Full rebuild runs on width change, mid-history rewrite (block count shrinks
// or transcriptDirty with non-append mutation), or when cache is empty.
// Append-only growth wraps only the new suffix of blocks.
func (m *chatTUI) clearWrapCache() {
	m.wrappedLines = nil
	m.wrapWidth = 0
	m.wrapBlockCount = 0
	m.wrapBlockLines = nil
}

// syncWrappedLines ensures wrappedLines matches m.transcript at contentW.
// Returns true when the viewport content string was rebuilt (caller should
// SetContent). forceFull rebuilds every block (width/theme/reflow).
func (m *chatTUI) syncWrappedLines(contentW int, forceFull bool) bool {
	if contentW <= 0 {
		contentW = 1
	}
	n := len(m.transcript)
	// Mid-history rewrite or width change → full rebuild.
	if forceFull || contentW != m.wrapWidth || n < m.wrapBlockCount || len(m.wrapBlockLines) != m.wrapBlockCount {
		return m.rebuildWrappedLinesFull(contentW)
	}
	if n == m.wrapBlockCount {
		// No new blocks; content may still have been edited in place via
		// setTranscriptBlock — those paths set transcriptDirty and forceFull
		// through the caller. Append-only no-op.
		return false
	}
	// Append-only: wrap each new block and extend the flat line list.
	// Equivalent to re-wrapping join(prefix, new…) for blocks that already
	// carry closed SGR and hard newlines (the streaming commit path).
	for i := m.wrapBlockCount; i < n; i++ {
		blockLines := wrapBlockLines(m.transcript[i], contentW)
		m.wrapBlockLines = append(m.wrapBlockLines, blockLines)
		if i == 0 && m.wrapBlockCount == 0 && len(m.wrappedLines) == 0 {
			m.wrappedLines = append([]string(nil), blockLines...)
			continue
		}
		// strings.Join inserts "\n" between blocks → concatenate line groups.
		m.wrappedLines = append(m.wrappedLines, blockLines...)
	}
	m.wrapBlockCount = n
	m.wrapWidth = contentW
	return true
}

func (m *chatTUI) rebuildWrappedLinesFull(contentW int) bool {
	if contentW <= 0 {
		contentW = 1
	}
	// Join-then-wrap is the historical source of truth for viewport content
	// (SGR balance across the full document). Per-block lines track append
	// positions for incremental extension.
	joined := strings.Join(m.transcript, "\n")
	wrapped := wrapTranscript(joined, contentW)
	m.wrappedLines = strings.Split(wrapped, "\n")
	n := len(m.transcript)
	m.wrapBlockLines = make([][]string, n)
	for i := 0; i < n; i++ {
		m.wrapBlockLines[i] = wrapBlockLines(m.transcript[i], contentW)
	}
	m.wrapBlockCount = n
	m.wrapWidth = contentW
	return true
}

// wrapBlockLines wraps one transcript block to width as a line slice.
func wrapBlockLines(block string, width int) []string {
	wrapped := wrapTranscript(block, width)
	if wrapped == "" {
		// Empty block is still one blank line in a Join(blocks, "\n") document
		// when it sits between neighbors; as a sole empty document it is "".
		return []string{""}
	}
	return strings.Split(wrapped, "\n")
}

// flattenBlockWraps joins per-block wrapped line slices the same way
// strings.Split(wrapTranscript(strings.Join(blocks, "\n"), w), "\n") would for
// already-hard-broken blocks: consecutive blocks become consecutive line groups.
func flattenBlockWraps(blocks [][]string) []string {
	if len(blocks) == 0 {
		return nil
	}
	// Estimate capacity.
	n := 0
	for _, b := range blocks {
		n += len(b)
	}
	out := make([]string, 0, n)
	for i, b := range blocks {
		if i > 0 {
			// strings.Join inserts "\n" between blocks. When both sides already
			// end/start as separate wrap lines, concatenating line slices is
			// enough — the join newline is the boundary between last line of
			// block i-1 and first line of block i, which is already how the
			// slices meet when we simply append.
		}
		out = append(out, b...)
	}
	return out
}

// wrappedContentString returns the viewport SetContent payload for the cache.
func (m chatTUI) wrappedContentString() string {
	if len(m.wrappedLines) == 0 {
		return ""
	}
	return strings.Join(m.wrappedLines, "\n")
}

// invalidateWrapFrom marks that block index (and after) must be rebuilt.
// Used after setTranscriptBlock / removeTranscriptBlock.
func (m *chatTUI) invalidateWrapFrom(index int) {
	if index < 0 {
		index = 0
	}
	if index >= m.wrapBlockCount {
		// Only trailing unknown blocks; append path will wrap them.
		return
	}
	// Truncate cache to the prefix before index so the next sync rebuilds
	// from there via full rebuild when counts mismatch, or re-wrap suffix.
	m.wrapBlockLines = m.wrapBlockLines[:index]
	m.wrapBlockCount = index
	m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
}
