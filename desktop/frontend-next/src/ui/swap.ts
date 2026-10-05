import { flushSync } from "react-dom";

// Swapping one view for another was a hard replace: the old one gone, the new
// one there, with nothing expressing the relation. Only the element being
// swapped carries a view-transition-name, and handing that name over lets the
// browser join the two frames into one action.
//
// flushSync is required — the callback has to finish mutating the DOM before it
// returns, or the transition captures the old frame.
/** The attribute on the root naming the swap in progress; removed when it ends. */
export const SWAP_MARK = "data-vt";

export function swapping(apply: () => void, kind: string) {
  if (!document.startViewTransition || matchMedia("(prefers-reduced-motion: reduce)").matches) {
    apply();
    return;
  }
  // Which switch this is has to reach the root: a named element is lifted out of
  // every transition regardless of what triggered it, so without the mark opening
  // settings would slide the pane too. An attribute predates view-transition
  // types by a long way, and this only needs that much of it.
  const root = document.documentElement;
  root.setAttribute(SWAP_MARK, kind);
  const vt = document.startViewTransition(() => flushSync(apply));
  // A transition the browser skips or aborts has still applied the swap; only
  // the animation was lost, which is not a failure worth an uncaught rejection.
  vt.ready.catch(() => {});
  vt.finished.finally(() => {
    if (root.getAttribute(SWAP_MARK) === kind) root.removeAttribute(SWAP_MARK);
  });
}
