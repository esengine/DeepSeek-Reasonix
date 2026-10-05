import { useEffect } from "react";
import type { RefObject } from "react";
import { listenAction } from "./listen";

// Closes a popover on an outside press or Escape. Both pickers in the model
// pane use it so a menu cannot be left hanging open behind the next click, and
// so neither has to reimplement the listener teardown.
//
// `also` is a second box that counts as inside. A menu rendered through a portal
// is not a DOM descendant of its trigger, so without it every click on the menu
// reads as a click away and closes the thing being clicked.
export function useDismiss(
  open: boolean,
  box: RefObject<HTMLElement | null>,
  close: () => void,
  also?: RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!box.current?.contains(t) && !also?.current?.contains(t)) close();
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      // The pane closes on Escape too; without this the menu and the whole
      // settings pane would both go on one press.
      e.stopPropagation();
      close();
    };
    // Both are the same intent reached two ways — pressing away and pressing
    // Escape — which is what the action vocabulary is for.
    const offAway = listenAction(window, "mousedown", { action: "layer.dismiss", listener: away as EventListener });
    const offEsc = listenAction(window, "keydown", { action: "layer.dismiss", listener: esc as EventListener }, true);
    return () => {
      offAway();
      offEsc();
    };
  }, [open, box, close, also]);
}

/** Escape closes this layer, and stops there.
 *
 *  The settings sheet listens for Escape on the window, so an inline form that
 *  does not claim the key first goes down with the whole sheet — and takes
 *  whatever was typed into it. Claiming it in the capture phase is what makes
 *  the innermost open thing the one that answers.
 */
export function useEscape(open: boolean, close: () => void) {
  useEffect(() => {
    if (!open) return;
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      close();
    };
    return listenAction(window, "keydown", { action: "layer.dismiss", listener: esc as EventListener }, true);
  }, [open, close]);
}
