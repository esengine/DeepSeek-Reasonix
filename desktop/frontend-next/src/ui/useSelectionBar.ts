import { useCallback, useEffect, useRef, useState } from "react";
import { listenAction } from "./listen";
import type { SelectionAt } from "./SelectionBar";

function scrollerTop(el: HTMLElement): number {
  for (let at: HTMLElement | null = el; at; at = at.parentElement) {
    if (/auto|scroll/.test(getComputedStyle(at).overflowY)) return at.getBoundingClientRect().top;
  }
  return 0;
}

/** Where the floating bar for a selection inside `box` belongs, once a pointer
 *  selection there has settled; null while there is none. */
export function useSelectionBar(box: React.RefObject<HTMLElement | null>, read: (box: HTMLElement) => string) {
  const [at, setAt] = useState<SelectionAt | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const clear = useCallback(() => { clearTimeout(timer.current); setAt(null); }, []);
  useEffect(() => {
    if (!at) return;
    const gone = () => {
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed) clear();
    };
    const stops = [
      listenAction(document, "selectionchange", { action: "reply.selection.bar", listener: gone }),
      listenAction(window, "scroll", { action: "reply.selection.bar", listener: clear }, true),
    ];
    return () => stops.forEach((stop) => stop());
  }, [at, clear]);
  useEffect(() => () => clearTimeout(timer.current), []);
  const settle = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const el = box.current;
      const text = el ? read(el) : "";
      if (!text) { setAt(null); return; }
      const range = document.getSelection()!.getRangeAt(0);
      const rect = typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : el!.getBoundingClientRect();
      setAt({ x: rect.left, top: rect.top, bottom: rect.bottom, floor: scrollerTop(el!), text });
    }, 120);
  }, [box, read]);
  return { at, settle, clear };
}
