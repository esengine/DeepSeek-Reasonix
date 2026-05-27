import { useCallback, useContext, useLayoutEffect, useRef } from 'react'
import { TerminalSizeContext } from '../components/TerminalSizeContext.js'
import type { DOMElement } from '../dom.js'

type ViewportEntry = {
  /** Whether the tracked element currently overlaps the terminal viewport. */
  isVisible: boolean
}

/** Track whether an element lives inside the terminal viewport. */
export function useTerminalViewport(): [
  ref: (element: DOMElement | null) => void,
  entry: ViewportEntry,
] {
  const terminalSize = useContext(TerminalSizeContext)
  const elementRef = useRef<DOMElement | null>(null)
  const entryRef = useRef<ViewportEntry>({ isVisible: true })

  const setElement = useCallback((el: DOMElement | null) => {
    elementRef.current = el
  }, [])

  useLayoutEffect(() => {
    const element = elementRef.current
    if (!element?.yogaNode || !terminalSize) {
      return
    }

    const height = element.yogaNode.getComputedHeight()
    const rows = terminalSize.rows

    // We walk the DOM parent chain rather than `yogaNode.getParent()`
    // because scroll containers track their own `scrollTop` outside of
    // Yoga's layout pass — Yoga reports positions assuming no scroll, and
    // scrollTop is applied at render time. Without this subtraction an
    // element inside a ScrollBox whose absolute Yoga position exceeds
    // `terminalRows` would be flagged offscreen even when scrolled into
    // view (e.g. the spinner in fullscreen mode after enough messages
    // accumulate).
    let absoluteTop = element.yogaNode.getComputedTop()
    let parent: DOMElement | undefined = element.parentNode
    let root = element.yogaNode
    while (parent) {
      if (parent.yogaNode) {
        absoluteTop += parent.yogaNode.getComputedTop()
        root = parent.yogaNode
      }
      // `scrollTop` is only ever set on scroll containers (by ScrollBox +
      // the renderer). Non-scroll nodes have undefined scrollTop, so the
      // falsy check is a fast path that avoids a per-frame subtract.
      if (parent.scrollTop) absoluteTop -= parent.scrollTop
      parent = parent.parentNode
    }

    // Only the root's height matters for the viewport math below.
    const screenHeight = root.getComputedHeight()

    const bottom = absoluteTop + height
    // When content overflows the viewport (`screenHeight > rows`), the
    // cursor-restore at the end of a frame scrolls one extra row into
    // scrollback. `log-update.ts` accounts for this with
    // `scrollbackRows = viewportY + 1` — we must match it, otherwise an
    // element straddling the boundary is "visible" here (animation keeps
    // ticking) while `log-update` treats its row as scrollback, producing
    // a full reset and visible flicker.
    const cursorRestoreScroll = screenHeight > rows ? 1 : 0
    const viewportY = Math.max(0, screenHeight - rows) + cursorRestoreScroll
    const viewportBottom = viewportY + rows
    const visible = bottom > viewportY && absoluteTop < viewportBottom

    if (visible !== entryRef.current.isVisible) {
      entryRef.current = { isVisible: visible }
    }
  })

  return [setElement, entryRef.current]
}
