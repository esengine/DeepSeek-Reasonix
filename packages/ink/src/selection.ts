/** Mouse-driven text selection state for alt-screen / fullscreen mode. */

import { clamp } from './layout/geometry.js'
import type { Screen, StylePool } from './screen.js'
import { CellWidth, cellAt, cellAtIndex, setCellStyleId } from './screen.js'

type Point = { col: number; row: number }

export type SelectionState = {
  /** Where the mouse-down occurred. Null when no selection. */
  anchor: Point | null
  /** Current drag position (updated on mouse-move while dragging). */
  focus: Point | null
  /** True between mouse-down and mouse-up. */
  isDragging: boolean
  anchorSpan: { lo: Point; hi: Point; kind: 'word' | 'line' } | null
  scrolledOffAbove: string[]
  /** Symmetric: rows scrolled out BELOW when dragging up. Appended. */
  scrolledOffBelow: string[]
  scrolledOffAboveSW: boolean[]
  /** Parallel to scrolledOffBelow. */
  scrolledOffBelowSW: boolean[]
  /** Pre-clamp anchor row. */
  virtualAnchorRow?: number
  /** Same for focus. */
  virtualFocusRow?: number
  lastPressHadAlt: boolean
}

export function createSelectionState(): SelectionState {
  return {
    anchor: null,
    focus: null,
    isDragging: false,
    anchorSpan: null,
    scrolledOffAbove: [],
    scrolledOffBelow: [],
    scrolledOffAboveSW: [],
    scrolledOffBelowSW: [],
    lastPressHadAlt: false,
  }
}

export function startSelection(
  s: SelectionState,
  col: number,
  row: number,
): void {
  s.anchor = { col, row }
  // Focus is not set until the first drag motion. A click-release with no
  // drag leaves focus null → hasSelection/selectionBounds return false/null
  // via the `!s.focus` check, so a bare click never highlights a cell.
  s.focus = null
  s.isDragging = true
  s.anchorSpan = null
  s.scrolledOffAbove = []
  s.scrolledOffBelow = []
  s.scrolledOffAboveSW = []
  s.scrolledOffBelowSW = []
  s.virtualAnchorRow = undefined
  s.virtualFocusRow = undefined
  s.lastPressHadAlt = false
}

export function updateSelection(
  s: SelectionState,
  col: number,
  row: number,
): void {
  if (!s.isDragging) return
  // First motion at the same cell as anchor is a no-op. Terminals in mode
  // 1002 can fire a drag event at the anchor cell (sub-pixel tremor, or a
  // motion-release pair). Setting focus here would turn a bare click into
  // a 1-cell selection and clobber the clipboard via useCopyOnSelect. Once
  // focus is set (real drag), we track normally including back to anchor.
  if (!s.focus && s.anchor && s.anchor.col === col && s.anchor.row === row)
    return
  s.focus = { col, row }
}

export function finishSelection(s: SelectionState): void {
  s.isDragging = false
  // Keep anchor/focus so highlight stays visible and text can be copied.
  // Clear via clearSelection() on Esc or after copy.
}

export function clearSelection(s: SelectionState): void {
  s.anchor = null
  s.focus = null
  s.isDragging = false
  s.anchorSpan = null
  s.scrolledOffAbove = []
  s.scrolledOffBelow = []
  s.scrolledOffAboveSW = []
  s.scrolledOffBelowSW = []
  s.virtualAnchorRow = undefined
  s.virtualFocusRow = undefined
  s.lastPressHadAlt = false
}

// Unicode-aware word-character matcher used by double-click word
// expansion. Letters in any script, digits, plus the small punctuation
// set that most popular terminal emulators treat as word-part. The
// trailing punctuation list (`/`, `-`, `+`, `~`, `_`, `.`, `\`) is
// chosen to keep paths like `/usr/local/bin` or `~/.config/foo.json`
// in a single word, which is the muscle memory most terminal users
// already have built up.
const WORD_CHAR = /[\p{L}\p{N}_/.\-+~\\]/u

/** Character class for double-click word-expansion. */
function charClass(c: string): 0 | 1 | 2 {
  if (c === ' ' || c === '') return 0
  if (WORD_CHAR.test(c)) return 1
  return 2
}

/** Find the bounds of the same-class character run at (col, row). */
function wordBoundsAt(
  screen: Screen,
  col: number,
  row: number,
): { lo: number; hi: number } | null {
  if (row < 0 || row >= screen.height) return null
  const width = screen.width
  const noSelect = screen.noSelect
  const rowOff = row * width

  // If the click landed on the spacer tail of a wide char, step back to
  // the head so the class check sees the actual grapheme.
  let c = col
  if (c > 0) {
    const cell = cellAt(screen, c, row)
    if (cell && cell.width === CellWidth.SpacerTail) c -= 1
  }
  if (c < 0 || c >= width || noSelect[rowOff + c] === 1) return null

  const startCell = cellAt(screen, c, row)
  if (!startCell) return null
  const cls = charClass(startCell.char)

  // Expand left: include cells of the same class, stop at noSelect or
  // class change. SpacerTail cells are stepped over (the wide-char head
  // at the preceding column determines the class).
  let lo = c
  while (lo > 0) {
    const prev = lo - 1
    if (noSelect[rowOff + prev] === 1) break
    const pc = cellAt(screen, prev, row)
    if (!pc) break
    if (pc.width === CellWidth.SpacerTail) {
      // Step over the spacer to the wide-char head
      if (prev === 0 || noSelect[rowOff + prev - 1] === 1) break
      const head = cellAt(screen, prev - 1, row)
      if (!head || charClass(head.char) !== cls) break
      lo = prev - 1
      continue
    }
    if (charClass(pc.char) !== cls) break
    lo = prev
  }

  // Expand right: same logic, skipping spacer tails.
  let hi = c
  while (hi < width - 1) {
    const next = hi + 1
    if (noSelect[rowOff + next] === 1) break
    const nc = cellAt(screen, next, row)
    if (!nc) break
    if (nc.width === CellWidth.SpacerTail) {
      // Include the spacer tail in the selection range (it belongs to
      // the wide char at hi) and continue past it.
      hi = next
      continue
    }
    if (charClass(nc.char) !== cls) break
    hi = next
  }

  return { lo, hi }
}

/** -1 if a < b, 1 if a > b, 0 if equal (reading order: row then col). */
function comparePoints(a: Point, b: Point): number {
  if (a.row !== b.row) return a.row < b.row ? -1 : 1
  if (a.col !== b.col) return a.col < b.col ? -1 : 1
  return 0
}

export function selectWordAt(
  s: SelectionState,
  screen: Screen,
  col: number,
  row: number,
): void {
  const b = wordBoundsAt(screen, col, row)
  if (!b) return
  const lo = { col: b.lo, row }
  const hi = { col: b.hi, row }
  s.anchor = lo
  s.focus = hi
  s.isDragging = true
  s.anchorSpan = { lo, hi, kind: 'word' }
}

// Printable ASCII minus terminal URL delimiters. Restricting to single-
// codeunit ASCII keeps cell-count === string-index, so the column-span
// check below is exact (no wide-char/grapheme drift).
const URL_BOUNDARY = new Set([...'<>"\'` '])
function isUrlChar(c: string): boolean {
  if (c.length !== 1) return false
  const code = c.charCodeAt(0)
  return code >= 0x21 && code <= 0x7e && !URL_BOUNDARY.has(c)
}

/** Scan the screen buffer for a plain-text URL at (col, row). */
export function findPlainTextUrlAt(
  screen: Screen,
  col: number,
  row: number,
): string | undefined {
  if (row < 0 || row >= screen.height) return undefined
  const width = screen.width
  const noSelect = screen.noSelect
  const rowOff = row * width

  let c = col
  if (c > 0) {
    const cell = cellAt(screen, c, row)
    if (cell && cell.width === CellWidth.SpacerTail) c -= 1
  }
  if (c < 0 || c >= width || noSelect[rowOff + c] === 1) return undefined

  const startCell = cellAt(screen, c, row)
  if (!startCell || !isUrlChar(startCell.char)) return undefined

  // Expand left/right to the bounds of the URL-char run. URLs are ASCII
  // (CellWidth.Narrow, 1 codeunit), so hitting a non-ASCII/wide/spacer
  // cell is a boundary — no need to step over spacers like wordBoundsAt.
  let lo = c
  while (lo > 0) {
    const prev = lo - 1
    if (noSelect[rowOff + prev] === 1) break
    const pc = cellAt(screen, prev, row)
    if (!pc || pc.width !== CellWidth.Narrow || !isUrlChar(pc.char)) break
    lo = prev
  }
  let hi = c
  while (hi < width - 1) {
    const next = hi + 1
    if (noSelect[rowOff + next] === 1) break
    const nc = cellAt(screen, next, row)
    if (!nc || nc.width !== CellWidth.Narrow || !isUrlChar(nc.char)) break
    hi = next
  }

  let token = ''
  for (let i = lo; i <= hi; i++) token += cellAt(screen, i, row)!.char

  // 1 cell = 1 char across [lo, hi] (ASCII-only run), so string index =
  // column offset. Find the last scheme anchor at or before the click —
  // a run like `https://a.com,https://b.com` has two, and clicking the
  // second should return the second URL, not the greedy match of both.
  const clickIdx = c - lo
  const schemeRe = /(?:https?|file):\/\//g
  let urlStart = -1
  let urlEnd = token.length
  for (let m; (m = schemeRe.exec(token)); ) {
    if (m.index > clickIdx) {
      urlEnd = m.index
      break
    }
    urlStart = m.index
  }
  if (urlStart < 0) return undefined
  let url = token.slice(urlStart, urlEnd)

  // Strip trailing sentence punctuation. For closers () ] }, only strip
  // if unbalanced — `/wiki/Foo_(bar)` keeps `)`, `/arr[0]` keeps `]`.
  const OPENER: Record<string, string> = { ')': '(', ']': '[', '}': '{' }
  while (url.length > 0) {
    const last = url.at(-1)!
    if ('.,;:!?'.includes(last)) {
      url = url.slice(0, -1)
      continue
    }
    const opener = OPENER[last]
    if (!opener) break
    let opens = 0
    let closes = 0
    for (let i = 0; i < url.length; i++) {
      const ch = url.charAt(i)
      if (ch === opener) opens++
      else if (ch === last) closes++
    }
    if (closes > opens) url = url.slice(0, -1)
    else break
  }

  // urlStart already guarantees click >= URL start; check right edge.
  if (clickIdx >= urlStart + url.length) return undefined

  return url
}

/** Select the entire row. */
export function selectLineAt(
  s: SelectionState,
  screen: Screen,
  row: number,
): void {
  if (row < 0 || row >= screen.height) return
  const lo = { col: 0, row }
  const hi = { col: screen.width - 1, row }
  s.anchor = lo
  s.focus = hi
  s.isDragging = true
  s.anchorSpan = { lo, hi, kind: 'line' }
}

/** Extend a word/line-mode selection to the word/line at (col, row). */
export function extendSelection(
  s: SelectionState,
  screen: Screen,
  col: number,
  row: number,
): void {
  if (!s.isDragging || !s.anchorSpan) return
  const span = s.anchorSpan
  let mLo: Point
  let mHi: Point
  if (span.kind === 'word') {
    const b = wordBoundsAt(screen, col, row)
    mLo = { col: b ? b.lo : col, row }
    mHi = { col: b ? b.hi : col, row }
  } else {
    const r = clamp(row, 0, screen.height - 1)
    mLo = { col: 0, row: r }
    mHi = { col: screen.width - 1, row: r }
  }
  if (comparePoints(mHi, span.lo) < 0) {
    // Mouse target ends before anchor span: extend backward.
    s.anchor = span.hi
    s.focus = mLo
  } else if (comparePoints(mLo, span.hi) > 0) {
    // Mouse target starts after anchor span: extend forward.
    s.anchor = span.lo
    s.focus = mHi
  } else {
    // Mouse overlaps the anchor span: just select the anchor span.
    s.anchor = span.lo
    s.focus = span.hi
  }
}

/** Semantic keyboard focus moves. */
export type FocusMove =
  | 'left'
  | 'right'
  | 'up'
  | 'down'
  | 'lineStart'
  | 'lineEnd'

/** Set focus to (col, row) for keyboard selection extension (shift+arrow). */
export function moveFocus(s: SelectionState, col: number, row: number): void {
  if (!s.focus) return
  s.anchorSpan = null
  s.focus = { col, row }
  // Explicit user repositioning — any stale virtual focus (from a prior
  // shiftSelection clamp) no longer reflects intent. Anchor stays put so
  // virtualAnchorRow is still valid for its own round-trip.
  s.virtualFocusRow = undefined
}

/** Shift anchor AND focus by dRow, clamped to [minRow, maxRow]. */
export function shiftSelection(
  s: SelectionState,
  dRow: number,
  minRow: number,
  maxRow: number,
  width: number,
): void {
  if (!s.anchor || !s.focus) return
  // Virtual rows track pre-clamp positions so reverse scrolls restore
  // correctly. Without this, clamp(5→0) + shift(+10) = 10, not the true 5,
  // and scrolledOffAbove stays stale (highlight ≠ copy).
  const vAnchor = (s.virtualAnchorRow ?? s.anchor.row) + dRow
  const vFocus = (s.virtualFocusRow ?? s.focus.row) + dRow
  if (
    (vAnchor < minRow && vFocus < minRow) ||
    (vAnchor > maxRow && vFocus > maxRow)
  ) {
    clearSelection(s)
    return
  }
  // Debt = how far the nearer endpoint overshoots each edge. When debt
  // shrinks (reverse scroll), those rows are back on-screen — pop from
  // the accumulator so getSelectedText doesn't double-count them.
  const oldMin = Math.min(
    s.virtualAnchorRow ?? s.anchor.row,
    s.virtualFocusRow ?? s.focus.row,
  )
  const oldMax = Math.max(
    s.virtualAnchorRow ?? s.anchor.row,
    s.virtualFocusRow ?? s.focus.row,
  )
  const oldAboveDebt = Math.max(0, minRow - oldMin)
  const oldBelowDebt = Math.max(0, oldMax - maxRow)
  const newAboveDebt = Math.max(0, minRow - Math.min(vAnchor, vFocus))
  const newBelowDebt = Math.max(0, Math.max(vAnchor, vFocus) - maxRow)
  if (newAboveDebt < oldAboveDebt) {
    // scrolledOffAbove pushes newest at the end (closest to on-screen).
    const drop = oldAboveDebt - newAboveDebt
    s.scrolledOffAbove.length -= drop
    s.scrolledOffAboveSW.length = s.scrolledOffAbove.length
  }
  if (newBelowDebt < oldBelowDebt) {
    // scrolledOffBelow unshifts newest at the front (closest to on-screen).
    const drop = oldBelowDebt - newBelowDebt
    s.scrolledOffBelow.splice(0, drop)
    s.scrolledOffBelowSW.splice(0, drop)
  }
  // Invariant: accumulator length ≤ debt. If the accumulator exceeds debt,
  // the excess is stale — e.g., moveFocus cleared virtualFocusRow without
  // trimming the accumulator, orphaning entries the pop above can never
  // reach because oldDebt was ALREADY 0. Truncate to debt (keeping the
  // newest = closest-to-on-screen entries). Check newDebt (not oldDebt):
  // captureScrolledRows runs BEFORE this shift in the real flow (ink.tsx),
  // so at entry the accumulator is populated but oldDebt is still 0 —
  // that's the normal establish-debt path, not stale.
  if (s.scrolledOffAbove.length > newAboveDebt) {
    // Above pushes newest at END → keep END.
    s.scrolledOffAbove =
      newAboveDebt > 0 ? s.scrolledOffAbove.slice(-newAboveDebt) : []
    s.scrolledOffAboveSW =
      newAboveDebt > 0 ? s.scrolledOffAboveSW.slice(-newAboveDebt) : []
  }
  if (s.scrolledOffBelow.length > newBelowDebt) {
    // Below unshifts newest at FRONT → keep FRONT.
    s.scrolledOffBelow = s.scrolledOffBelow.slice(0, newBelowDebt)
    s.scrolledOffBelowSW = s.scrolledOffBelowSW.slice(0, newBelowDebt)
  }
  // Clamp col depends on which EDGE (not dRow direction): virtual tracking
  // means a top-clamped point can stay top-clamped during a dRow>0 reverse
  // shift — dRow-based clampCol would give it the bottom col.
  const shift = (p: Point, vRow: number): Point => {
    if (vRow < minRow) return { col: 0, row: minRow }
    if (vRow > maxRow) return { col: width - 1, row: maxRow }
    return { col: p.col, row: vRow }
  }
  s.anchor = shift(s.anchor, vAnchor)
  s.focus = shift(s.focus, vFocus)
  s.virtualAnchorRow =
    vAnchor < minRow || vAnchor > maxRow ? vAnchor : undefined
  s.virtualFocusRow = vFocus < minRow || vFocus > maxRow ? vFocus : undefined
  // anchorSpan not virtual-tracked: it's for word/line extend-on-drag,
  // irrelevant to the keyboard-scroll round-trip case.
  if (s.anchorSpan) {
    const sp = (p: Point): Point => {
      const r = p.row + dRow
      if (r < minRow) return { col: 0, row: minRow }
      if (r > maxRow) return { col: width - 1, row: maxRow }
      return { col: p.col, row: r }
    }
    s.anchorSpan = {
      lo: sp(s.anchorSpan.lo),
      hi: sp(s.anchorSpan.hi),
      kind: s.anchorSpan.kind,
    }
  }
}

/** Shift the anchor row by dRow, clamped to [minRow, maxRow]. */
export function shiftAnchor(
  s: SelectionState,
  dRow: number,
  minRow: number,
  maxRow: number,
): void {
  if (!s.anchor) return
  // Same virtual-row tracking as shiftSelection/shiftSelectionForFollow: the
  // drag→follow transition hands off to shiftSelectionForFollow, which reads
  // (virtualAnchorRow ?? anchor.row). Without this, drag-phase clamping
  // leaves virtual undefined → follow initializes from the already-clamped
  // row, under-counting total drift → shiftSelection's invariant-restore
  // prematurely clears valid drag-phase accumulator entries.
  const raw = (s.virtualAnchorRow ?? s.anchor.row) + dRow
  s.anchor = { col: s.anchor.col, row: clamp(raw, minRow, maxRow) }
  s.virtualAnchorRow = raw < minRow || raw > maxRow ? raw : undefined
  // anchorSpan not virtual-tracked (word/line extend, irrelevant to
  // keyboard-scroll round-trip) — plain clamp from current row.
  if (s.anchorSpan) {
    const shift = (p: Point): Point => ({
      col: p.col,
      row: clamp(p.row + dRow, minRow, maxRow),
    })
    s.anchorSpan = {
      lo: shift(s.anchorSpan.lo),
      hi: shift(s.anchorSpan.hi),
      kind: s.anchorSpan.kind,
    }
  }
}

export function shiftSelectionForFollow(
  s: SelectionState,
  dRow: number,
  minRow: number,
  maxRow: number,
): boolean {
  if (!s.anchor) return false
  // Mirror shiftSelection: compute raw (unclamped) positions from virtual
  // if set, else current. This handles BOTH the update path (virtual already
  // set from a prior keyboard scroll) AND the initialize path (first clamp
  // happens HERE via follow-scroll, no prior keyboard scroll). Without the
  // initialize path, follow-scroll-first leaves virtual undefined even
  // though the clamp below occurred → a later PgUp computes debt from the
  // clamped row instead of the true pre-clamp row and never pops the
  // accumulator — getSelectedText double-counts the off-screen rows.
  const rawAnchor = (s.virtualAnchorRow ?? s.anchor.row) + dRow
  const rawFocus = s.focus
    ? (s.virtualFocusRow ?? s.focus.row) + dRow
    : undefined
  if (rawAnchor < minRow && rawFocus !== undefined && rawFocus < minRow) {
    clearSelection(s)
    return true
  }
  // Clamp from raw, not p.row+dRow — so a virtual position coming back
  // in-bounds lands at the TRUE position, not the stale clamped one.
  s.anchor = { col: s.anchor.col, row: clamp(rawAnchor, minRow, maxRow) }
  if (s.focus && rawFocus !== undefined) {
    s.focus = { col: s.focus.col, row: clamp(rawFocus, minRow, maxRow) }
  }
  s.virtualAnchorRow =
    rawAnchor < minRow || rawAnchor > maxRow ? rawAnchor : undefined
  s.virtualFocusRow =
    rawFocus !== undefined && (rawFocus < minRow || rawFocus > maxRow)
      ? rawFocus
      : undefined
  // anchorSpan not virtual-tracked (word/line extend, irrelevant to
  // keyboard-scroll round-trip) — plain clamp from current row.
  if (s.anchorSpan) {
    const shift = (p: Point): Point => ({
      col: p.col,
      row: clamp(p.row + dRow, minRow, maxRow),
    })
    s.anchorSpan = {
      lo: shift(s.anchorSpan.lo),
      hi: shift(s.anchorSpan.hi),
      kind: s.anchorSpan.kind,
    }
  }
  return false
}

export function hasSelection(s: SelectionState): boolean {
  return s.anchor !== null && s.focus !== null
}

/** Normalized selection bounds: start is always before end in reading order. */
export function selectionBounds(s: SelectionState): {
  start: { col: number; row: number }
  end: { col: number; row: number }
} | null {
  if (!s.anchor || !s.focus) return null
  return comparePoints(s.anchor, s.focus) <= 0
    ? { start: s.anchor, end: s.focus }
    : { start: s.focus, end: s.anchor }
}

/** Check if a cell at (col, row) is within the current selection range. */
export function isCellSelected(
  s: SelectionState,
  col: number,
  row: number,
): boolean {
  const b = selectionBounds(s)
  if (!b) return false
  const { start, end } = b
  if (row < start.row || row > end.row) return false
  if (row === start.row && col < start.col) return false
  if (row === end.row && col > end.col) return false
  return true
}

/** Extract text from one screen row. */
function extractRowText(
  screen: Screen,
  row: number,
  colStart: number,
  colEnd: number,
): string {
  const noSelect = screen.noSelect
  const rowOff = row * screen.width
  const contentEnd = row + 1 < screen.height ? screen.softWrap[row + 1]! : 0
  const lastCol = contentEnd > 0 ? Math.min(colEnd, contentEnd - 1) : colEnd
  let line = ''
  for (let col = colStart; col <= lastCol; col++) {
    // Skip cells marked noSelect (gutters, line numbers, diff sigils).
    // Check before cellAt to avoid the decode cost for excluded cells.
    if (noSelect[rowOff + col] === 1) continue
    const cell = cellAt(screen, col, row)
    if (!cell) continue
    // Skip spacer tails (second half of wide chars) — the head already
    // contains the full grapheme. SpacerHead is a blank at line-end.
    if (
      cell.width === CellWidth.SpacerTail ||
      cell.width === CellWidth.SpacerHead
    ) {
      continue
    }
    line += cell.char
  }
  return contentEnd > 0 ? line : line.replace(/\s+$/, '')
}

function joinRows(
  lines: string[],
  text: string,
  sw: boolean | undefined,
): void {
  if (sw && lines.length > 0) {
    lines[lines.length - 1] += text
  } else {
    lines.push(text)
  }
}

/** Extract text from the screen buffer within the selection range. */
export function getSelectedText(s: SelectionState, screen: Screen): string {
  const b = selectionBounds(s)
  if (!b) return ''
  const { start, end } = b
  const sw = screen.softWrap
  const lines: string[] = []

  for (let i = 0; i < s.scrolledOffAbove.length; i++) {
    joinRows(lines, s.scrolledOffAbove[i]!, s.scrolledOffAboveSW[i])
  }

  for (let row = start.row; row <= end.row; row++) {
    const rowStart = row === start.row ? start.col : 0
    const rowEnd = row === end.row ? end.col : screen.width - 1
    joinRows(lines, extractRowText(screen, row, rowStart, rowEnd), sw[row]! > 0)
  }

  for (let i = 0; i < s.scrolledOffBelow.length; i++) {
    joinRows(lines, s.scrolledOffBelow[i]!, s.scrolledOffBelowSW[i])
  }

  return lines.join('\n')
}

export function captureScrolledRows(
  s: SelectionState,
  screen: Screen,
  firstRow: number,
  lastRow: number,
  side: 'above' | 'below',
): void {
  const b = selectionBounds(s)
  if (!b || firstRow > lastRow) return
  const { start, end } = b
  // Intersect [firstRow, lastRow] with [start.row, end.row]. Rows outside
  // the selection aren't captured — they weren't selected.
  const lo = Math.max(firstRow, start.row)
  const hi = Math.min(lastRow, end.row)
  if (lo > hi) return

  const width = screen.width
  const sw = screen.softWrap
  const captured: string[] = []
  const capturedSW: boolean[] = []
  for (let row = lo; row <= hi; row++) {
    const colStart = row === start.row ? start.col : 0
    const colEnd = row === end.row ? end.col : width - 1
    captured.push(extractRowText(screen, row, colStart, colEnd))
    capturedSW.push(sw[row]! > 0)
  }

  if (side === 'above') {
    // Newest rows go at the bottom of the above-accumulator (closest to
    // the on-screen content in reading order).
    s.scrolledOffAbove.push(...captured)
    s.scrolledOffAboveSW.push(...capturedSW)
    // We just captured the top of the selection. The anchor (=start when
    // dragging down) is now pointing at content that will scroll out; its
    // col constraint was applied to the captured row. Reset to col 0 so
    // the NEXT tick and the final getSelectedText read the full row.
    if (s.anchor && s.anchor.row === start.row && lo === start.row) {
      s.anchor = { col: 0, row: s.anchor.row }
      if (s.anchorSpan) {
        s.anchorSpan = {
          kind: s.anchorSpan.kind,
          lo: { col: 0, row: s.anchorSpan.lo.row },
          hi: { col: width - 1, row: s.anchorSpan.hi.row },
        }
      }
    }
  } else {
    // Newest rows go at the TOP of the below-accumulator — they're
    // closest to the on-screen content.
    s.scrolledOffBelow.unshift(...captured)
    s.scrolledOffBelowSW.unshift(...capturedSW)
    if (s.anchor && s.anchor.row === end.row && hi === end.row) {
      s.anchor = { col: width - 1, row: s.anchor.row }
      if (s.anchorSpan) {
        s.anchorSpan = {
          kind: s.anchorSpan.kind,
          lo: { col: 0, row: s.anchorSpan.lo.row },
          hi: { col: width - 1, row: s.anchorSpan.hi.row },
        }
      }
    }
  }
}

export function applySelectionOverlay(
  screen: Screen,
  selection: SelectionState,
  stylePool: StylePool,
): void {
  const b = selectionBounds(selection)
  if (!b) return
  const { start, end } = b
  const width = screen.width
  const noSelect = screen.noSelect
  for (let row = start.row; row <= end.row && row < screen.height; row++) {
    const colStart = row === start.row ? start.col : 0
    const colEnd = row === end.row ? Math.min(end.col, width - 1) : width - 1
    const rowOff = row * width
    for (let col = colStart; col <= colEnd; col++) {
      const idx = rowOff + col
      // Skip noSelect cells — gutters stay visually unchanged so it's
      // clear they're not part of the copy. Surrounding selectable cells
      // still highlight so the selection extent remains visible.
      if (noSelect[idx] === 1) continue
      const cell = cellAtIndex(screen, idx)
      setCellStyleId(screen, col, row, stylePool.withSelectionBg(cell.styleId))
    }
  }
}
