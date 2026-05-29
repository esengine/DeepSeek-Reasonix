import { useCallback, useContext, useLayoutEffect, useRef } from 'react'
import CursorDeclarationContext from '../components/CursorDeclarationContext.js'
import type { DOMElement } from '../dom.js'

type UseDeclaredCursorOptions = {
  line: number
  column: number
  active: boolean
}

export function useDeclaredCursor({
  line,
  column,
  active,
}: UseDeclaredCursorOptions): (element: DOMElement | null) => void {
  const setCursorDeclaration = useContext(CursorDeclarationContext)
  const nodeRef = useRef<DOMElement | null>(null)
  const prevRef = useRef<{ line: number; column: number; active: boolean } | null>(null)

  const setNode = useCallback((node: DOMElement | null) => {
    nodeRef.current = node
  }, [])

  // When active, set only if position actually changed. When inactive,
  // clear only if the currently-declared node is ours — the node-identity
  // check guards two hazards:
  //
  //   1. A memoized active instance elsewhere (e.g. a search input inside
  //      a memo'd Footer) doesn't re-render this commit; an inactive
  //      instance re-rendering here must not clobber it.
  //   2. Sibling handoff (menu focus moving between list items) — when
  //      focus moves opposite to sibling order, the newly-inactive item's
  //      effect runs AFTER the newly-active item's set. Without the node
  //      check it would clobber the freshly-claimed declaration.
  //
  // The position-comparison guard prevents redundant set → null → set
  // churn on every render, which caused visible cursor jitter when IME
  // preedit committed text (the component re-renders but the cursor
  // column is unchanged — skipping the write avoids a frame where the
  // terminal briefly parks the cursor at a wrong intermediate spot).
  useLayoutEffect(() => {
    const prev = prevRef.current
    const positionUnchanged =
      prev !== null && prev.active === active && prev.line === line && prev.column === column
    if (positionUnchanged) return

    prevRef.current = { line, column, active }
    const node = nodeRef.current
    if (active && node) {
      setCursorDeclaration({ relativeX: column, relativeY: line, node })
    } else {
      setCursorDeclaration(null, node)
    }
  })

  // Unmount cleanup is conditional too — by the time we unmount, another
  // instance may already own the declaration. Kept in a separate effect
  // with `[setCursorDeclaration]` so the cleanup runs only once at
  // unmount, never on every line/column change (which would transiently
  // null the declaration between commits).
  useLayoutEffect(() => {
    return () => {
      setCursorDeclaration(null, nodeRef.current)
    }
  }, [setCursorDeclaration])

  return setNode
}
