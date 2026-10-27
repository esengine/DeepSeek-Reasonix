import { type RefObject, useEffect } from "react";

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

// Dialogs that are open right now, innermost last. Only the innermost one keeps
// Tab: an outer dialog that also pulled focus back would fight the one on top.
const open: HTMLElement[] = [];

// Drawn, as opposed to present: a collapsed fold or a hidden row is in the tree
// and is not a stop. Computed style rather than layout, so it reads the same in a
// test renderer that never lays anything out.
function drawn(el: HTMLElement, root: HTMLElement): boolean {
  if (el.closest("[hidden],[inert]") || getComputedStyle(el).visibility === "hidden") return false;
  for (let n: HTMLElement | null = el; n && n !== root.parentElement; n = n.parentElement) {
    if (getComputedStyle(n).display === "none") return false;
  }
  return true;
}

const reachable = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => drawn(el, root));

/** Keeps Tab inside a dialog that declares itself modal, and gives focus back to
 *  whatever had it when the dialog opened. Without it a keyboard user tabs out of
 *  the sheet into the page it covers and cannot see where focus went. The opener
 *  gets its ring back only if it had one: a mouse click that opened the dialog
 *  must not turn into a keyboard-focus look because Esc closed it. */
export function useModalFocus(ref: RefObject<HTMLElement | null>, active = true) {
  useEffect(() => {
    const root = ref.current;
    if (!active || !root) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const ringed = opener?.matches(":focus-visible") ?? false;
    open.push(root);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || open[open.length - 1] !== root) return;
      const stops = reachable(root);
      if (stops.length === 0) {
        e.preventDefault();
        root.focus();
        return;
      }
      const first = stops[0];
      const last = stops[stops.length - 1];
      const at = document.activeElement;
      if (!(at instanceof Node) || !root.contains(at)) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
      } else if (e.shiftKey && (at === first || at === root)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && at === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      const i = open.lastIndexOf(root);
      if (i >= 0) open.splice(i, 1);
      if (opener?.isConnected) opener.focus(ringed ? { preventScroll: true } : { preventScroll: true, focusVisible: false });
    };
  }, [ref, active]);
}
