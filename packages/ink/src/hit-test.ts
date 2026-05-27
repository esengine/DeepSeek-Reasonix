import type { DOMElement } from './dom.js';
import { ClickEvent } from './events/click-event.js';
import type { EventHandlerProps } from './events/event-handlers.js';
import { nodeCache } from './node-cache.js';

/** Find the deepest DOM element whose painted rectangle contains (col, row). */
export function hitTest(
  node: DOMElement,
  col: number,
  row: number,
): DOMElement | null {
  const rect = nodeCache.get(node);
  if (!rect) return null;
  if (
    col < rect.x ||
    col >= rect.x + rect.width ||
    row < rect.y ||
    row >= rect.y + rect.height
  ) {
    return null;
  }
  for (let i = node.childNodes.length - 1; i >= 0; i--) {
    const child = node.childNodes[i]!;
    if (child.nodeName === '#text') continue;
    const hit = hitTest(child, col, row);
    if (hit) return hit;
  }
  return node;
}

/** Dispatch a synthetic click at (col, row) against the rendered tree. */
export function dispatchClick(
  root: DOMElement,
  col: number,
  row: number,
  cellIsBlank = false,
): boolean {
  let target: DOMElement | undefined = hitTest(root, col, row) ?? undefined;
  if (!target) return false;

  // The root is always ink-root, which holds the FocusManager. Walk up
  // looking for a focusable ancestor and promote it.
  if (root.focusManager) {
    let focusTarget: DOMElement | undefined = target;
    while (focusTarget) {
      if (typeof focusTarget.attributes['tabIndex'] === 'number') {
        root.focusManager.handleClickFocus(focusTarget);
        break;
      }
      focusTarget = focusTarget.parentNode;
    }
  }
  const event = new ClickEvent(col, row, cellIsBlank);
  let handled = false;
  while (target) {
    const handler = target._eventHandlers?.onClick as
      | ((event: ClickEvent) => void)
      | undefined;
    if (handler) {
      handled = true;
      // Recompute local coordinates for each handler — they're relative
      // to the element actually handling the event, not the original hit.
      const rect = nodeCache.get(target);
      if (rect) {
        event.localCol = col - rect.x;
        event.localRow = row - rect.y;
      }
      handler(event);
      if (event.didStopImmediatePropagation()) return true;
    }
    target = target.parentNode;
  }
  return handled;
}

/** Fire onMouseEnter / onMouseLeave as the pointer moves over the tree. */
export function dispatchHover(
  root: DOMElement,
  col: number,
  row: number,
  hovered: Set<DOMElement>,
): void {
  const next = new Set<DOMElement>();
  let node: DOMElement | undefined = hitTest(root, col, row) ?? undefined;
  while (node) {
    const h = node._eventHandlers as EventHandlerProps | undefined;
    if (h?.onMouseEnter || h?.onMouseLeave) next.add(node);
    node = node.parentNode;
  }
  for (const old of hovered) {
    if (!next.has(old)) {
      hovered.delete(old);
      // If the node was unmounted between pointer events, it has no
      // parent any more; skip the handler rather than firing into a
      // detached subtree.
      if (old.parentNode) {
        (old._eventHandlers as EventHandlerProps | undefined)?.onMouseLeave?.();
      }
    }
  }
  for (const n of next) {
    if (!hovered.has(n)) {
      hovered.add(n);
      (n._eventHandlers as EventHandlerProps | undefined)?.onMouseEnter?.();
    }
  }
}
