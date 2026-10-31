import { useLayoutEffect, useRef, type RefObject } from "react";
import { useTeamMotion } from "../../state/teamMotion";

// How far a row is drawn from its place, by a slide still in flight.
function drift(row: HTMLElement): number {
  const t = getComputedStyle(row).transform;
  if (!t || t === "none" || typeof DOMMatrixReadOnly !== "function") return 0;
  return new DOMMatrixReadOnly(t).m42;
}

// Rows that changed place slide from where they were. Only a change of place
// moves anything: a row met for the first time, on mount or when it is added,
// has no earlier place and stays still. Places are read against the list, so
// scrolling the rail is no change of place, and with the slide in flight
// taken out, so a reorder landing mid-slide continues from where the row is.
export function useReorderMotion(list: RefObject<HTMLElement | null>, order: string) {
  const places = useRef<Map<string, number> | null>(null);
  const motion = useTeamMotion();
  useLayoutEffect(() => {
    const el = list.current;
    if (!el) return;
    const rows = [...el.children].filter((r): r is HTMLElement => r instanceof HTMLElement && !!r.dataset.key);
    const origin = el.getBoundingClientRect().top;
    const drawn = new Map(rows.map((r) => [r.dataset.key!, drift(r)]));
    const now = new Map(rows.map((r) => [r.dataset.key!, r.getBoundingClientRect().top - origin - drawn.get(r.dataset.key!)!]));
    const before = places.current;
    places.current = now;
    const reduced = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!before || motion === "quiet" || reduced) return;
    for (const row of rows) {
      const key = row.dataset.key!;
      const was = before.get(key);
      const is = now.get(key);
      if (was === undefined || is === undefined || was === is || typeof row.animate !== "function") continue;
      const from = was + drawn.get(key)! - is;
      for (const a of row.getAnimations?.() ?? []) a.cancel();
      row.animate([{ transform: `translateY(${from}px)` }, { transform: "translateY(0)" }], { duration: 280, easing: "cubic-bezier(.2, .8, .25, 1.15)" });
    }
  }, [list, order, motion]);
}
