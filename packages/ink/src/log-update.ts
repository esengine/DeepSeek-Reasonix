/** Inline (non-alt-screen) frame writer. */

import {
  type AnsiCode,
  ansiCodesToString,
  diffAnsiCodes,
} from '@alcalzone/ansi-tokenize'
import { logForDebugging } from './_internal/debug.js'
import type { Diff, FlickerReason, Frame } from './frame.js'
import type { Point } from './layout/geometry.js'
import {
  type Cell,
  CellWidth,
  cellAt,
  charInCellAt,
  diffEach,
  type Hyperlink,
  isEmptyCellAt,
  type Screen,
  type StylePool,
  shiftRows,
  visibleCellAtIndex,
} from './screen.js'
import {
  CURSOR_HOME,
  scrollDown as csiScrollDown,
  scrollUp as csiScrollUp,
  RESET_SCROLL_REGION,
  setScrollRegion,
} from './termio/csi.js'
import { LINK_END, link as oscLink } from './termio/osc.js'

type State = {
  previousOutput: string
}

type Options = {
  isTTY: boolean
  stylePool: StylePool
}

const CARRIAGE_RETURN = { type: 'carriageReturn' } as const
const NEWLINE = { type: 'stdout', content: '\n' } as const

export class LogUpdate {
  private state: State

  constructor(private readonly options: Options) {
    this.state = {
      previousOutput: '',
    }
  }

  renderPreviousOutput_DEPRECATED(prevFrame: Frame): Diff {
    if (!this.options.isTTY) {
      // Non-TTY output is no longer supported (string output was removed)
      return [NEWLINE]
    }
    return this.getRenderOpsForDone(prevFrame)
  }

  // Called on SIGCONT. While the process was stopped the user (or their
  // shell) may have written into the block we thought we owned, so the
  // cached previous output is no longer trustworthy and we have to fall
  // back to a full repaint on the next frame.
  reset(): void {
    this.state.previousOutput = ''
  }

  private renderFullFrame(frame: Frame): Diff {
    const { screen } = frame
    const lines: string[] = []
    let currentStyles: AnsiCode[] = []
    let currentHyperlink: Hyperlink = undefined
    for (let y = 0; y < screen.height; y++) {
      let line = ''
      for (let x = 0; x < screen.width; x++) {
        const cell = cellAt(screen, x, y)
        if (cell && cell.width !== CellWidth.SpacerTail) {
          // Handle hyperlink transitions
          if (cell.hyperlink !== currentHyperlink) {
            if (currentHyperlink !== undefined) {
              line += LINK_END
            }
            if (cell.hyperlink !== undefined) {
              line += oscLink(cell.hyperlink)
            }
            currentHyperlink = cell.hyperlink
          }
          const cellStyles = this.options.stylePool.get(cell.styleId)
          const styleDiff = diffAnsiCodes(currentStyles, cellStyles)
          if (styleDiff.length > 0) {
            line += ansiCodesToString(styleDiff)
            currentStyles = cellStyles
          }
          line += cell.char
        }
      }
      // Close any open hyperlink before resetting styles
      if (currentHyperlink !== undefined) {
        line += LINK_END
        currentHyperlink = undefined
      }
      // Reset styles at end of line so trimEnd doesn't leave dangling codes
      const resetCodes = diffAnsiCodes(currentStyles, [])
      if (resetCodes.length > 0) {
        line += ansiCodesToString(resetCodes)
        currentStyles = []
      }
      lines.push(line.trimEnd())
    }

    if (lines.length === 0) {
      return []
    }
    return [{ type: 'stdout', content: lines.join('\n') }]
  }

  private getRenderOpsForDone(prev: Frame): Diff {
    this.state.previousOutput = ''

    if (!prev.cursor.visible) {
      return [{ type: 'cursorShow' }]
    }
    return []
  }

  render(
    prev: Frame,
    next: Frame,
    altScreen = false,
    decstbmSafe = true,
  ): Diff {
    if (!this.options.isTTY) {
      return this.renderFullFrame(next)
    }

    const startTime = performance.now()
    const stylePool = this.options.stylePool

    // The inline strategy assumes the cursor is at the bottom of our
    // block, so the only resize cases we actually have to handle are
    // "viewport got shorter" (cursor drifts up off our block) and
    // "viewport got narrower" (wrapping changes shift content). Predicting
    // the post-resize wrap state without a full layout pass is hopelessly
    // expensive, and resizes are rare in practice, so we take the cheap
    // route here and force a full reset.
    if (
      next.viewport.height < prev.viewport.height ||
      (prev.viewport.width !== 0 && next.viewport.width !== prev.viewport.width)
    ) {
      return fullResetSequence_CAUSES_FLICKER(next, 'resize', stylePool)
    }

    // Hardware-scroll fast path. When a ScrollBox's `scrollTop` changes
    // we can tell the terminal to scroll the affected region itself
    // (DECSTBM `CSI top;bot r` followed by `CSI n S` or `T`) instead of
    // re-writing every row that moved. We then mirror that shift on
    // `prev.screen` so the cell diff below naturally sees only the rows
    // that were freshly *uncovered* by the scroll. Mutating prev is fine
    // here because the caller is about to roll it into `backFrame` for
    // the next render.
    //
    // The trailing `CURSOR_HOME` after `RESET_SCROLL_REGION` is defensive:
    // the spec says DECSTBM reset homes the cursor, but real terminals
    // disagree.
    //
    // `decstbmSafe` is the gate for atomicity. Callers pass `false` when
    // the host terminal doesn't support DEC 2026 / BSU+ESU; without
    // atomicity the outer terminal can paint the intermediate state
    // (region scrolled, freshly-revealed rows still blank) and the user
    // sees a vertical jump on every frame where `scrollTop` moves. The
    // fallback re-writes every shifted row — more bytes, no flash. Either
    // way `next.screen` (produced by `render-node-to-output`'s blit+shift)
    // is the correct final state.
    let scrollPatch: Diff = []
    if (altScreen && next.scrollHint && decstbmSafe) {
      const { top, bottom, delta } = next.scrollHint
      if (
        top >= 0 &&
        bottom < prev.screen.height &&
        bottom < next.screen.height
      ) {
        shiftRows(prev.screen, top, bottom, delta)
        scrollPatch = [
          {
            type: 'stdout',
            content:
              setScrollRegion(top + 1, bottom + 1) +
              (delta > 0 ? csiScrollUp(delta) : csiScrollDown(-delta)) +
              RESET_SCROLL_REGION +
              CURSOR_HOME,
          },
        ]
      }
    }

    // All cursor moves below are relative — in inline mode the absolute
    // starting column/row of our block is unknown.
    //
    // The next branch handles the case where the previous frame already
    // filled (or overflowed) the viewport and the cursor sits at the
    // bottom. In that situation the cursor-restore at the end of the
    // previous frame issued an LF that scrolled one extra row into
    // scrollback on top of whatever rows had already been pushed out by
    // content overflow. If any change in the new frame touches a row
    // that is now in scrollback, we cannot reach it with cursor moves —
    // a full reset is the only correct option.
    //
    // We only run this early check in steady-state (not growing). The
    // growing case is covered later by the `viewportY` computation with
    // `cursorRestoreScroll`, which catches unreachable rows in the
    // diff loop itself.
    const cursorAtBottom = prev.cursor.y >= prev.screen.height
    const isGrowing = next.screen.height > prev.screen.height
    // When content fills the viewport exactly (height == viewport) and the
    // cursor is at the bottom, the cursor-restore LF at the end of the
    // previous frame scrolled 1 row into scrollback. Use >= to catch this.
    const prevHadScrollback =
      cursorAtBottom && prev.screen.height >= prev.viewport.height
    const isShrinking = next.screen.height < prev.screen.height
    const nextFitsViewport = next.screen.height <= prev.viewport.height

    // Shrinking from "taller than viewport" to "fits in viewport" means
    // rows that were previously in scrollback should now be on screen.
    // No terminal escape can pull content back out of scrollback, so the
    // only way to put the user in the right state is a full reset.
    // The comparison is `<=` rather than `<` because even when the new
    // screen height exactly equals the viewport height, the scrollback
    // depth left by the previous render differs from a fresh render's.
    if (prevHadScrollback && nextFitsViewport && isShrinking) {
      logForDebugging(
        `Full reset (shrink->below): prevHeight=${prev.screen.height}, nextHeight=${next.screen.height}, viewport=${prev.viewport.height}`,
      )
      return fullResetSequence_CAUSES_FLICKER(next, 'offscreen', stylePool)
    }

    if (
      prev.screen.height >= prev.viewport.height &&
      prev.screen.height > 0 &&
      cursorAtBottom &&
      !isGrowing
    ) {
      // viewportY = rows in scrollback from content overflow
      // +1 for the row pushed by cursor-restore scroll
      const viewportY = prev.screen.height - prev.viewport.height
      const scrollbackRows = viewportY + 1

      let scrollbackChangeY = -1
      diffEach(prev.screen, next.screen, (_x, y) => {
        if (y < scrollbackRows) {
          scrollbackChangeY = y
          return true // early exit
        }
      })
      if (scrollbackChangeY >= 0) {
        const prevLine = readLine(prev.screen, scrollbackChangeY)
        const nextLine = readLine(next.screen, scrollbackChangeY)
        return fullResetSequence_CAUSES_FLICKER(next, 'offscreen', stylePool, {
          triggerY: scrollbackChangeY,
          prevLine,
          nextLine,
        })
      }
    }

    const screen = new VirtualScreen(prev.cursor, next.viewport.width)

    // Treat empty screen as height 1 to avoid spurious adjustments on first render
    const heightDelta =
      Math.max(next.screen.height, 1) - Math.max(prev.screen.height, 1)
    const shrinking = heightDelta < 0
    const growing = heightDelta > 0

    // Handle shrinking: clear lines from the bottom
    if (shrinking) {
      const linesToClear = prev.screen.height - next.screen.height

      // eraseLines only works within the viewport - it can't clear scrollback.
      // If we need to clear more lines than fit in the viewport, some are in
      // scrollback, so we need a full reset.
      if (linesToClear > prev.viewport.height) {
        return fullResetSequence_CAUSES_FLICKER(
          next,
          'offscreen',
          this.options.stylePool,
        )
      }

      // clear(N) moves cursor UP by N-1 lines and to column 0
      // This puts us at line prev.screen.height - N = next.screen.height
      // But we want to be at next.screen.height - 1 (bottom of new screen)
      screen.txn(prev => [
        [
          { type: 'clear', count: linesToClear },
          { type: 'cursorMove', x: 0, y: -1 },
        ],
        { dx: -prev.x, dy: -linesToClear },
      ])
    }

    // `viewportY` is the count of rows currently sitting in scrollback —
    // i.e. above the visible top of the terminal. The choice of basis
    // depends on direction:
    //   - Shrinking: take `max(prev, next)` because terminal clear ops
    //     never scroll, so the previously-pushed rows remain in
    //     scrollback even after the visible portion contracts.
    //   - Growing: take the prev state, because the new rows have not
    //     yet been added and therefore have not scrolled anything out
    //     of view.
    // When `prevHadScrollback` is set, we add 1 for the cursor-restore
    // LF emitted at the end of the previous frame — it scrolled one
    // additional row past the top of the visible window. Without that
    // bias, the diff loop would treat the row as still reachable; the
    // cursor would clamp at the viewport top and writes would land one
    // row low, garbling the frame.
    const cursorRestoreScroll = prevHadScrollback ? 1 : 0
    const viewportY = growing
      ? Math.max(
          0,
          prev.screen.height - prev.viewport.height + cursorRestoreScroll,
        )
      : Math.max(prev.screen.height, next.screen.height) -
        next.viewport.height +
        cursorRestoreScroll

    let currentStyleId = stylePool.none
    let currentHyperlink: Hyperlink = undefined

    // First pass: render changes to existing rows (rows < prev.screen.height)
    let needsFullReset = false
    let resetTriggerY = -1
    diffEach(prev.screen, next.screen, (x, y, removed, added) => {
      // Skip new rows - we'll render them directly after
      if (growing && y >= prev.screen.height) {
        return
      }

      // Skip spacers during rendering because the terminal will automatically
      // advance 2 columns when we write the wide character itself.
      // SpacerTail: Second cell of a wide character
      // SpacerHead: Marks line-end position where wide char wraps to next line
      if (
        added &&
        (added.width === CellWidth.SpacerTail ||
          added.width === CellWidth.SpacerHead)
      ) {
        return
      }

      if (
        removed &&
        (removed.width === CellWidth.SpacerTail ||
          removed.width === CellWidth.SpacerHead) &&
        !added
      ) {
        return
      }

      // Skip empty cells that don't need to overwrite existing content.
      // This prevents writing trailing spaces that would cause unnecessary
      // line wrapping at the edge of the screen.
      // Uses isEmptyCellAt to check if both packed words are zero (empty cell).
      if (added && isEmptyCellAt(next.screen, x, y) && !removed) {
        return
      }

      // If the cell outside the viewport range has changed, we need to reset
      // because we can't move the cursor there to draw.
      if (y < viewportY) {
        needsFullReset = true
        resetTriggerY = y
        return true // early exit
      }

      moveCursorTo(screen, x, y)

      if (added) {
        const targetHyperlink = added.hyperlink
        currentHyperlink = transitionHyperlink(
          screen.diff,
          currentHyperlink,
          targetHyperlink,
        )
        const styleStr = stylePool.transition(currentStyleId, added.styleId)
        if (writeCellWithStyleStr(screen, added, styleStr)) {
          currentStyleId = added.styleId
        }
      } else if (removed) {
        // Cell was removed - clear it with a space
        // (This handles shrinking content)
        // Reset any active styles/hyperlinks first to avoid leaking into cleared cells
        const styleIdToReset = currentStyleId
        const hyperlinkToReset = currentHyperlink
        currentStyleId = stylePool.none
        currentHyperlink = undefined

        screen.txn(() => {
          const patches: Diff = []
          transitionStyle(patches, stylePool, styleIdToReset, stylePool.none)
          transitionHyperlink(patches, hyperlinkToReset, undefined)
          patches.push({ type: 'stdout', content: ' ' })
          return [patches, { dx: 1, dy: 0 }]
        })
      }
    })
    if (needsFullReset) {
      return fullResetSequence_CAUSES_FLICKER(next, 'offscreen', stylePool, {
        triggerY: resetTriggerY,
        prevLine: readLine(prev.screen, resetTriggerY),
        nextLine: readLine(next.screen, resetTriggerY),
      })
    }

    // Reset styles before rendering new rows (they'll set their own styles)
    currentStyleId = transitionStyle(
      screen.diff,
      stylePool,
      currentStyleId,
      stylePool.none,
    )
    currentHyperlink = transitionHyperlink(
      screen.diff,
      currentHyperlink,
      undefined,
    )

    // Handle growth: render new rows directly (they naturally scroll the terminal)
    if (growing) {
      renderFrameSlice(
        screen,
        next,
        prev.screen.height,
        next.screen.height,
        stylePool,
      )
    }

    // Restore the cursor for the next frame. Two regimes:
    //   - Alt-screen: skip entirely. The cursor is hidden, its position
    //     only matters as the start of the *next* frame's relative
    //     moves, and the next frame in alt-screen always begins with
    //     `CSI H` (see ink.tsx onRender) which homes to (0,0) no matter
    //     where we leave it. Skipping saves a CR plus cursorMove —
    //     roughly 6-10 bytes — every frame.
    //   - Main screen: if the cursor needs to be one row past the last
    //     line of content (typical `cursor.y = screen.height`), emit a
    //     `\n` to create that line. Cursor-move escapes cannot create
    //     new lines, only `LF` can.
    if (altScreen) {
      // no-op; next frame's CSI H anchors cursor
    } else if (next.cursor.y >= next.screen.height) {
      // Move to column 0 of current line, then emit newlines to reach target row
      screen.txn(prev => {
        const rowsToCreate = next.cursor.y - prev.y
        if (rowsToCreate > 0) {
          // Use CR to resolve pending wrap (if any) without advancing
          // to the next line, then LF to create each new row.
          const patches: Diff = new Array<Diff[number]>(1 + rowsToCreate)
          patches[0] = CARRIAGE_RETURN
          for (let i = 0; i < rowsToCreate; i++) {
            patches[1 + i] = NEWLINE
          }
          return [patches, { dx: -prev.x, dy: rowsToCreate }]
        }
        // At or past target row - need to move cursor to correct position
        const dy = next.cursor.y - prev.y
        if (dy !== 0 || prev.x !== next.cursor.x) {
          // Use CR to clear pending wrap (if any), then cursor move
          const patches: Diff = [CARRIAGE_RETURN]
          patches.push({ type: 'cursorMove', x: next.cursor.x, y: dy })
          return [patches, { dx: next.cursor.x - prev.x, dy }]
        }
        return [[], { dx: 0, dy: 0 }]
      })
    } else {
      moveCursorTo(screen, next.cursor.x, next.cursor.y)
    }

    const elapsed = performance.now() - startTime
    if (elapsed > 50) {
      const damage = next.screen.damage
      const damageInfo = damage
        ? `${damage.width}x${damage.height} at (${damage.x},${damage.y})`
        : 'none'
      logForDebugging(
        `Slow render: ${elapsed.toFixed(1)}ms, screen: ${next.screen.height}x${next.screen.width}, damage: ${damageInfo}, changes: ${screen.diff.length}`,
      )
    }

    return scrollPatch.length > 0
      ? [...scrollPatch, ...screen.diff]
      : screen.diff
  }
}

function transitionHyperlink(
  diff: Diff,
  current: Hyperlink,
  target: Hyperlink,
): Hyperlink {
  if (current !== target) {
    diff.push({ type: 'hyperlink', uri: target ?? '' })
    return target
  }
  return current
}

function transitionStyle(
  diff: Diff,
  stylePool: StylePool,
  currentId: number,
  targetId: number,
): number {
  const str = stylePool.transition(currentId, targetId)
  if (str.length > 0) {
    diff.push({ type: 'styleStr', str })
  }
  return targetId
}

function readLine(screen: Screen, y: number): string {
  let line = ''
  for (let x = 0; x < screen.width; x++) {
    line += charInCellAt(screen, x, y) ?? ' '
  }
  return line.trimEnd()
}

function fullResetSequence_CAUSES_FLICKER(
  frame: Frame,
  reason: FlickerReason,
  stylePool: StylePool,
  debug?: { triggerY: number; prevLine: string; nextLine: string },
): Diff {
  // After clearTerminal, cursor is at (0, 0)
  const screen = new VirtualScreen({ x: 0, y: 0 }, frame.viewport.width)
  renderFrame(screen, frame, stylePool)
  return [{ type: 'clearTerminal', reason, debug }, ...screen.diff]
}

function renderFrame(
  screen: VirtualScreen,
  frame: Frame,
  stylePool: StylePool,
): void {
  renderFrameSlice(screen, frame, 0, frame.screen.height, stylePool)
}

/** Render a slice of rows from the frame's screen. */
function renderFrameSlice(
  screen: VirtualScreen,
  frame: Frame,
  startY: number,
  endY: number,
  stylePool: StylePool,
): VirtualScreen {
  let currentStyleId = stylePool.none
  let currentHyperlink: Hyperlink = undefined
  // Track the styleId of the last rendered cell on this line (-1 if none).
  // Passed to visibleCellAtIndex to enable fg-only space optimization.
  let lastRenderedStyleId = -1

  const { width: screenWidth, cells, charPool, hyperlinkPool } = frame.screen

  let index = startY * screenWidth
  for (let y = startY; y < endY; y += 1) {
    // Advance to this row via LF, *not* `CSI CUD` (cursor-down). CSI CUD
    // clamps at the viewport bottom margin and cannot scroll, whereas
    // LF scrolls the viewport and creates a fresh line. If we used CUD
    // here, calls made while the cursor sits at the bottom margin would
    // silently no-op and the virtual cursor would drift permanently
    // off-by-one against the real one.
    if (screen.cursor.y < y) {
      const rowsToAdvance = y - screen.cursor.y
      screen.txn(prev => {
        const patches: Diff = new Array<Diff[number]>(1 + rowsToAdvance)
        patches[0] = CARRIAGE_RETURN
        for (let i = 0; i < rowsToAdvance; i++) {
          patches[1 + i] = NEWLINE
        }
        return [patches, { dx: -prev.x, dy: rowsToAdvance }]
      })
    }
    // Reset at start of each line — no cell rendered yet
    lastRenderedStyleId = -1

    for (let x = 0; x < screenWidth; x += 1, index += 1) {
      // Skip spacers, unstyled empty cells, and fg-only styled spaces that
      // match the last rendered style (since cursor-forward produces identical
      // visual result). visibleCellAtIndex handles the optimization internally
      // to avoid allocating Cell objects for skipped cells.
      const cell = visibleCellAtIndex(
        cells,
        charPool,
        hyperlinkPool,
        index,
        lastRenderedStyleId,
      )
      if (!cell) {
        continue
      }

      moveCursorTo(screen, x, y)

      // Handle hyperlink
      const targetHyperlink = cell.hyperlink
      currentHyperlink = transitionHyperlink(
        screen.diff,
        currentHyperlink,
        targetHyperlink,
      )

      // Style transition — cached string, zero allocations after warmup
      const styleStr = stylePool.transition(currentStyleId, cell.styleId)
      if (writeCellWithStyleStr(screen, cell, styleStr)) {
        currentStyleId = cell.styleId
        lastRenderedStyleId = cell.styleId
      }
    }
    // Reset styles/hyperlinks before newline so background color doesn't
    // bleed into the next line when the terminal scrolls. The old code
    // reset implicitly by writing trailing unstyled spaces; now that we
    // skip empty cells, we must reset explicitly.
    currentStyleId = transitionStyle(
      screen.diff,
      stylePool,
      currentStyleId,
      stylePool.none,
    )
    currentHyperlink = transitionHyperlink(
      screen.diff,
      currentHyperlink,
      undefined,
    )
    // CR+LF at end of row — \r resets to column 0, \n moves to next line.
    // Without \r, the terminal cursor stays at whatever column content ended
    // (since we skip trailing spaces, this can be mid-row).
    screen.txn(prev => [[CARRIAGE_RETURN, NEWLINE], { dx: -prev.x, dy: 1 }])
  }

  // Reset any open style/hyperlink at end of slice
  transitionStyle(screen.diff, stylePool, currentStyleId, stylePool.none)
  transitionHyperlink(screen.diff, currentHyperlink, undefined)

  return screen
}

type Delta = { dx: number; dy: number }

function writeCellWithStyleStr(
  screen: VirtualScreen,
  cell: Cell,
  styleStr: string,
): boolean {
  const cellWidth = cell.width === CellWidth.Wide ? 2 : 1
  const px = screen.cursor.x
  const vw = screen.viewportWidth

  // Don't write wide chars that would cross the viewport edge.
  // Single-codepoint chars (CJK) at vw-2 are safe; multi-codepoint
  // graphemes (flags, ZWJ emoji) need stricter threshold.
  if (cellWidth === 2 && px < vw) {
    const threshold = cell.char.length > 2 ? vw : vw + 1
    if (px + 2 >= threshold) {
      return false
    }
  }

  const diff = screen.diff
  if (styleStr.length > 0) {
    diff.push({ type: 'styleStr', str: styleStr })
  }

  const needsCompensation = cellWidth === 2 && needsWidthCompensation(cell.char)

  // On terminals with old wcwidth tables, a compensated emoji only advances
  // the cursor 1 column, so the CHA below skips column x+1 without painting
  // it. Write a styled space there first — on correct terminals the emoji
  // glyph (width 2) overwrites it harmlessly; on old terminals it fills the
  // gap with the emoji's background. Also clears any stale content at x+1.
  // CHA is 1-based, so column px+1 (0-based) is CHA target px+2.
  if (needsCompensation && px + 1 < vw) {
    diff.push({ type: 'cursorTo', col: px + 2 })
    diff.push({ type: 'stdout', content: ' ' })
    diff.push({ type: 'cursorTo', col: px + 1 })
  }

  diff.push({ type: 'stdout', content: cell.char })

  // Force terminal cursor to correct column after the emoji.
  if (needsCompensation) {
    diff.push({ type: 'cursorTo', col: px + cellWidth + 1 })
  }

  // Update cursor — mutate in place to avoid Point allocation
  if (px >= vw) {
    screen.cursor.x = cellWidth
    screen.cursor.y++
  } else {
    screen.cursor.x = px + cellWidth
  }
  return true
}

function moveCursorTo(screen: VirtualScreen, targetX: number, targetY: number) {
  screen.txn(prev => {
    const dx = targetX - prev.x
    const dy = targetY - prev.y
    const inPendingWrap = prev.x >= screen.viewportWidth

    // If we're in pending wrap state (cursor.x >= width), use CR
    // to reset to column 0 on the current line without advancing
    // to the next line, then issue the cursor movement.
    if (inPendingWrap) {
      return [
        [CARRIAGE_RETURN, { type: 'cursorMove', x: targetX, y: dy }],
        { dx, dy },
      ]
    }

    // When moving to a different line, use carriage return (\r) to reset to
    // column 0 first, then cursor move.
    if (dy !== 0) {
      return [
        [CARRIAGE_RETURN, { type: 'cursorMove', x: targetX, y: dy }],
        { dx, dy },
      ]
    }

    // Standard same-line cursor move
    return [[{ type: 'cursorMove', x: dx, y: dy }], { dx, dy }]
  })
}

/** Identify emoji where the terminal's wcwidth may disagree with Unicode. */
function needsWidthCompensation(char: string): boolean {
  const cp = char.codePointAt(0)
  if (cp === undefined) return false
  // U+1FA70-U+1FAFF: Symbols and Pictographs Extended-A (Unicode 12.0-15.0)
  // U+1FB00-U+1FBFF: Symbols for Legacy Computing (Unicode 13.0)
  if ((cp >= 0x1fa70 && cp <= 0x1faff) || (cp >= 0x1fb00 && cp <= 0x1fbff)) {
    return true
  }
  // Text-by-default emoji with VS16: scan for U+FE0F in multi-codepoint
  // graphemes. Single BMP chars (length 1) and surrogate pairs without VS16
  // skip this check. VS16 (0xFE0F) can't collide with surrogates (0xD800-0xDFFF).
  if (char.length >= 2) {
    for (let i = 0; i < char.length; i++) {
      if (char.charCodeAt(i) === 0xfe0f) return true
    }
  }
  return false
}

class VirtualScreen {
  // Public for direct mutation by writeCellWithStyleStr (avoids txn overhead).
  // File-private class — not exposed outside log-update.ts.
  cursor: Point
  diff: Diff = []

  constructor(
    origin: Point,
    readonly viewportWidth: number,
  ) {
    this.cursor = { ...origin }
  }

  txn(fn: (prev: Point) => [patches: Diff, next: Delta]): void {
    const [patches, next] = fn(this.cursor)
    for (const patch of patches) {
      this.diff.push(patch)
    }
    this.cursor.x += next.dx
    this.cursor.y += next.dy
  }
}
