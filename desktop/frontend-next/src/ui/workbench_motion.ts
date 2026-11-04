import { useEffect, useRef, useState, type RefObject } from "react";
import { listenAction } from "./listen";

const still = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

/** Tabs that appeared after the strip was first drawn, for as long as they take
 *  to come in. A tab drawn with the strip did not just open, so it does not move. */
export function useEnteringTabs(keys: string[]): ReadonlySet<string> {
  const seen = useRef<Set<string> | null>(null);
  const [entering, setEntering] = useState<ReadonlySet<string>>(() => new Set());
  const joined = keys.join("\n");
  useEffect(() => {
    const now = new Set(joined ? joined.split("\n") : []);
    const before = seen.current;
    seen.current = now;
    if (!before) return;
    const fresh = [...now].filter((k) => !before.has(k));
    if (!fresh.length) return;
    setEntering(new Set(fresh));
    const id = setTimeout(() => setEntering(new Set()), 220);
    return () => clearTimeout(id);
  }, [joined]);
  return entering;
}

/** Narrows a closing tab to nothing before it is removed; at once when motion is reduced. */
export function leave(el: Element | null, then: () => void) {
  if (!(el instanceof HTMLElement) || typeof el.animate !== "function" || still()) return then();
  el.animate([{ maxWidth: `${el.offsetWidth}px`, opacity: 1 }, { maxWidth: "0px", opacity: 0 }], { duration: 140, easing: "ease-out" }).finished.then(then, then);
}

/** Fades the canvas in when the selected tab changes, never when it is first drawn. */
export function useSwitchFade(ref: RefObject<HTMLElement | null>, key: string) {
  const last = useRef(key);
  useEffect(() => {
    if (last.current === key) return;
    last.current = key;
    const el = ref.current;
    if (el && typeof el.animate === "function" && !still()) el.animate([{ opacity: 0.35 }, { opacity: 1 }], { duration: 120, easing: "ease-out" });
  }, [ref, key]);
}

/** The tab strip's ref, and the tabs it cannot show at its width, in strip order. */
export function useOverflowTabs(keys: string[]): [RefObject<HTMLDivElement | null>, string[]] {
  const strip = useRef<HTMLDivElement>(null);
  const [hidden, setHidden] = useState<string[]>([]);
  const joined = keys.join("\n");
  useEffect(() => {
    const stripEl = strip.current;
    if (!stripEl) return;
    const measure = () => {
      const box = stripEl.getBoundingClientRect();
      const out = [...stripEl.querySelectorAll<HTMLElement>("[data-key]")]
        .filter((tab) => { const r = tab.getBoundingClientRect(); return r.width > 0 && (r.right > box.right + 1 || r.left < box.left - 1); })
        .map((tab) => tab.dataset.key!);
      setHidden((was) => (was.join("\n") === out.join("\n") ? was : out));
    };
    measure();
    const ro = typeof ResizeObserver === "function" ? new ResizeObserver(measure) : null;
    ro?.observe(stripEl);
    const stop = listenAction(stripEl, "scroll", { action: "workbench.tab-overflow", listener: measure }, { passive: true });
    return () => { ro?.disconnect(); stop(); };
  }, [joined]);
  return [strip, hidden];
}

/** Slides the workbench in from the right when it is brought out, never when it is
 *  drawn already out, so a window that opens with it open does not move. */
export function useOpenSlide(ref: RefObject<HTMLElement | null>, shown: boolean) {
  const was = useRef(shown);
  useEffect(() => {
    const opened = !was.current && shown;
    was.current = shown;
    const el = ref.current;
    if (opened && el && typeof el.animate === "function" && !still()) {
      el.animate([{ transform: "translateX(24px)", opacity: 0 }, { transform: "none", opacity: 1 }], { duration: 200, easing: "cubic-bezier(.2,0,0,1)" });
    }
  }, [ref, shown]);
}

/** Slides the workbench back out before it is put away; at once when motion is reduced. */
export function slideAway(el: HTMLElement | null, then: () => void) {
  if (!el || typeof el.animate !== "function" || still()) return then();
  el.animate([{ transform: "none", opacity: 1 }, { transform: "translateX(24px)", opacity: 0 }], { duration: 160, easing: "ease-in" }).finished.then(then, then);
}
