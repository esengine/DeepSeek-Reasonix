/**
 * Nested scroll handoff for transcript reading.
 *
 * Trackpads latch wheel events to the first overflow≠visible ancestor under the
 * pointer — including elements that only set overflow-x:auto (CSS then computes
 * overflow-y to auto). When that ancestor cannot scroll further in the gesture
 * direction, continued wheels stall instead of moving the outer transcript.
 *
 * This helper promotes those edge / non-scrollable vertical wheels to the
 * parent scroller and latches to the parent for the rest of the gesture.
 */

export const NESTED_SCROLL_ATTR = "data-nested-scroll";

const EDGE_EPSILON_PX = 1;
const DEFAULT_LINE_HEIGHT_PX = 16;
const DOM_DELTA_PIXEL = 0;
const DOM_DELTA_PAGE = 2;

export type NestedScrollHandoffOptions = {
  /** Outer reading scroller (`.transcript`). */
  parent: HTMLElement;
  /** Called with normalized deltaY when a nested edge wheel is promoted. */
  onParentScrollIntent?: (deltaY: number) => void;
  /** Latch to the parent after the first edge handoff until this many ms of silence. */
  latchHoldMs?: number;
  now?: () => number;
};

export type NestedScrollHandoff = {
  detach: () => void;
};

/** Normalize WheelEvent line/page deltas before applying them to scrollTop. */
export function normalizeWheelDelta(event: Pick<WheelEvent, "deltaX" | "deltaY" | "deltaMode">, viewport: HTMLElement) {
  const deltaMode = event.deltaMode ?? DOM_DELTA_PIXEL;
  if (deltaMode === DOM_DELTA_PIXEL) {
    return { x: event.deltaX, y: event.deltaY };
  }
  if (deltaMode === DOM_DELTA_PAGE) {
    const page = Math.max(1, viewport.clientHeight);
    return { x: event.deltaX * page, y: event.deltaY * page };
  }
  const computed = typeof getComputedStyle === "function" ? getComputedStyle(viewport).lineHeight : "";
  const parsed = Number.parseFloat(computed);
  const line = Number.isFinite(parsed) && parsed > 0 ? parsed : DEFAULT_LINE_HEIGHT_PX;
  return { x: event.deltaX * line, y: event.deltaY * line };
}

function overflowAllowsScroll(value: string): boolean {
  return value === "auto" || value === "scroll" || value === "overlay";
}

/** True when the element is a CSS scroll container that can still move in deltaY. */
export function canElementScrollVertically(el: HTMLElement, deltaY: number): boolean {
  const style = typeof getComputedStyle === "function" ? getComputedStyle(el) : null;
  if (style && !overflowAllowsScroll(style.overflowY) && !overflowAllowsScroll(style.overflowX)) {
    // overflow-x:auto forces overflow-y to compute as auto in CSS; if both are
    // visible, this is not a scroll container.
    return false;
  }
  const overflow = el.scrollHeight - el.clientHeight;
  if (overflow <= EDGE_EPSILON_PX) return false;
  if (deltaY < 0) return el.scrollTop > EDGE_EPSILON_PX;
  if (deltaY > 0) return el.scrollTop + el.clientHeight < el.scrollHeight - EDGE_EPSILON_PX;
  return false;
}

/**
 * Walk from the event target up to (but not including) parent and return the
 * nearest element that can absorb this vertical delta. Prefers explicitly
 * marked `[data-nested-scroll]` nodes when they are on the path.
 */
export function findVerticalScrollTarget(
  target: EventTarget | null,
  parent: HTMLElement,
  deltaY: number,
): HTMLElement | null {
  if (!(target instanceof Element)) return null;
  let node: Element | null = target;
  while (node && node !== parent) {
    if (node instanceof HTMLElement && node !== parent) {
      if (canElementScrollVertically(node, deltaY)) return node;
    }
    node = node.parentElement;
  }
  return null;
}

/**
 * True when the event target sits under a nested overflow container that would
 * steal vertical trackpad gestures even though it cannot scroll further in Y.
 * Those are the "sticky table / sticky code" cases from the mac recording.
 */
export function shouldHandoffVerticalWheel(
  target: EventTarget | null,
  parent: HTMLElement,
  deltaY: number,
): boolean {
  if (!(target instanceof Element) || !parent.contains(target) || target === parent) return false;

  // Explicit markers always participate in handoff at their edge.
  const marked = target.closest(`[${NESTED_SCROLL_ATTR}]`);
  if (marked instanceof HTMLElement && parent.contains(marked) && marked !== parent) {
    return !canElementScrollVertically(marked, deltaY);
  }

  // Any intermediate overflow container that cannot scroll in this direction
  // will latch the trackpad; promote those wheels to the parent.
  let node: Element | null = target;
  while (node && node !== parent) {
    if (node instanceof HTMLElement) {
      const style = typeof getComputedStyle === "function" ? getComputedStyle(node) : null;
      if (style && (overflowAllowsScroll(style.overflowY) || overflowAllowsScroll(style.overflowX))) {
        // Found a nested scrollport. If it can still scroll in Y, leave it alone;
        // otherwise the gesture would stall on this node.
        return !canElementScrollVertically(node, deltaY);
      }
    }
    node = node.parentElement;
  }
  return false;
}

/**
 * Attach a capture-phase wheel listener on `parent` that handoffs edge wheels
 * from nested overflow containers to the parent. Returns a detach function.
 */
export function attachNestedScrollHandoff(options: NestedScrollHandoffOptions): NestedScrollHandoff {
  const {
    parent,
    onParentScrollIntent,
    latchHoldMs = 220,
    now = () => Date.now(),
  } = options;

  let latchUntil = 0;

  const onWheel = (event: WheelEvent) => {
    // Pinch-zoom is synthesized as ctrl+wheel on macOS trackpads.
    if (event.ctrlKey || event.defaultPrevented) return;
    const delta = normalizeWheelDelta(event, parent);
    if (delta.y === 0) return;
    if (Math.abs(delta.x) > Math.abs(delta.y)) return;

    const t = now();
    const latched = latchUntil > t;

    // After the first edge handoff, keep driving the parent for the rest of
    // this trackpad gesture so the user does not re-latch into the nested box.
    if (latched) {
      event.preventDefault();
      onParentScrollIntent?.(delta.y);
      parent.scrollTop += delta.y;
      latchUntil = t + latchHoldMs;
      return;
    }

    // Nested scroller can still absorb this delta — let the browser handle it.
    if (findVerticalScrollTarget(event.target, parent, delta.y)) return;

    if (!shouldHandoffVerticalWheel(event.target, parent, delta.y)) return;

    event.preventDefault();
    onParentScrollIntent?.(delta.y);
    parent.scrollTop += delta.y;
    latchUntil = t + latchHoldMs;
  };

  parent.addEventListener("wheel", onWheel, { capture: true, passive: false });
  return {
    detach: () => parent.removeEventListener("wheel", onWheel, { capture: true } as AddEventListenerOptions),
  };
}
