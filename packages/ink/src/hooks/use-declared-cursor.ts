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

  const setNode = useCallback((node: DOMElement | null) => {
    nodeRef.current = node
  }, [])

  // When active, set unconditionally. When inactive, clear only if the
  // currently-declared node is ours — the node-identity check guards two
  // hazards:
  //
  //   1. A memoized active instance elsewhere (e.g. a search input inside
  //      a memo'd Footer) doesn't re-render this commit; an inactive
  //      instance re-rendering here must not clobber it.
  //   2. Sibling handoff (menu focus moving between list items) — when
  //      focus moves opposite to sibling order, the newly-inactive item's
  //      effect runs AFTER the newly-active item's set. Without the node
  //      check it would clobber the freshly-claimed declaration.
  //
  // No dep array on purpose: this must re-declare every commit so the
  // active instance can re-claim ownership after a peer's unmount cleanup
  // or sibling handoff nulled it.
  useLayoutEffect(() => {
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
