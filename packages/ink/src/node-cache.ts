import type { DOMElement } from './dom.js';
import type { Rectangle } from './layout/geometry.js';

/** Last-committed screen geometry of a rendered node. */
export type CachedLayout = {
  x: number;
  y: number;
  width: number;
  height: number;
  top?: number;
};

export const nodeCache = new WeakMap<DOMElement, CachedLayout>();

export const pendingClears = new WeakMap<DOMElement, Rectangle[]>();

function findRoot(node: DOMElement): DOMElement | undefined {
  let current: DOMElement | undefined = node;
  while (current) {
    if (current.focusManager) return current;
    current = current.parentNode;
  }
  return undefined;
}

export function addPendingClear(
  parent: DOMElement,
  rect: Rectangle,
  isAbsolute: boolean,
): void {
  const existing = pendingClears.get(parent);
  if (existing) {
    existing.push(rect);
  } else {
    pendingClears.set(parent, [rect]);
  }

  if (isAbsolute) {
    // An absolute node can paint over cells outside its parent subtree —
    // say, an overlay drawn on top of a ScrollBox that appears earlier in
    // tree order. If we let the renderer's blit fast path restore
    // prevScreen for those untouched siblings we'd resurrect the overlay's
    // pixels and the user would see ghosting. Normal-flow removals don't
    // cross subtree boundaries and are handled by `hasRemovedChild` at
    // the parent level, so they don't trip this flag.
    //
    // Stored on the root rather than at module scope so two Ink instances
    // running on different streams cannot cross-contaminate.
    const root = findRoot(parent);
    if (root) root.absoluteNodeRemoved = true;
  }
}

export function consumeAbsoluteRemovedFlag(root: DOMElement): boolean {
  const had = root.absoluteNodeRemoved === true;
  root.absoluteNodeRemoved = false;
  return had;
}
