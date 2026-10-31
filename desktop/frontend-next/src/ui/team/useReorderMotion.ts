import { useLayoutEffect, useRef, type RefObject } from "react";
import { useTeamMotion } from "../../state/teamMotion";

// Rows that changed place slide from where they were. Only a change of place
// moves anything: a row met for the first time, on mount or when it is added,
// has no earlier place and stays still.
export function useReorderMotion(list: RefObject<HTMLElement | null>, order: string) {
  const tops = useRef<Map<string, number> | null>(null);
  const motion = useTeamMotion();
  useLayoutEffect(() => {
    const el = list.current;
    if (!el) return;
    const rows = [...el.children].filter((r): r is HTMLElement => r instanceof HTMLElement && !!r.dataset.key);
    const now = new Map(rows.map((r) => [r.dataset.key!, r.getBoundingClientRect().top]));
    const before = tops.current;
    tops.current = now;
    const reduced = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!before || motion === "quiet" || reduced) return;
    for (const row of rows) {
      const was = before.get(row.dataset.key!);
      const is = now.get(row.dataset.key!);
      if (was === undefined || is === undefined || was === is || typeof row.animate !== "function") continue;
      row.animate([{ transform: `translateY(${was - is}px)` }, { transform: "translateY(0)" }], { duration: 280, easing: "cubic-bezier(.2, .8, .25, 1.15)" });
    }
  }, [list, order, motion]);
}
