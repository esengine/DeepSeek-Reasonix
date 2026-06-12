import { useEffect, useRef } from "react";
import gsap from "gsap";
import { DUR_SLOW, EASE_OUT, prefersReducedMotion } from "./gsapAnimations";

/**
 * useEntranceAnimation — animates newly-mounted elements as they appear in
 * the DOM. Tracks seen item IDs so each element animates in only once.
 *
 * Key performance properties:
 *  - On first mount, ALL existing data-entrance IDs are pre-seeded into the
 *    "seen" set so no entrance animation runs for history items.
 *  - The scan only runs when `deps` changes (pass items.length or similar).
 *  - During streaming (text changes within same elements) the scanner is
 *    completely skipped, avoiding expensive querySelectorAll calls.
 *
 * Usage:
 *   const entranceRef = useEntranceAnimation(items.length);
 *   <div ref={entranceRef}>
 *     {items.map((it) => <div key={it.id} data-entrance={it.id} />)}
 *   </div>
 */
export function useEntranceAnimation<T extends HTMLElement>(
  deps?: unknown,
  selector = "[data-entrance]",
) {
  const ref = useRef<T | null>(null);
  const seen = useRef(new Set<string>());
  const timerRef = useRef<number | null>(null);
  const ready = useRef(false);

  // Pre-seed: on first mount, record all existing data-entrance IDs so they
  // never get an entrance animation.  Only newly added DOM nodes animate.
  useEffect(() => {
    const container = ref.current;
    if (!container) return;
    container.querySelectorAll(selector).forEach((el) => {
      const id = el.getAttribute("data-entrance");
      if (id) seen.current.add(id);
    });
    ready.current = true;
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Subsequent renders: scan for elements NOT in the seen set.
  useEffect(() => {
    const container = ref.current;
    if (!container || !ready.current) return;

    const entries: HTMLElement[] = [];
    container.querySelectorAll(selector).forEach((el) => {
      const id = el.getAttribute("data-entrance");
      if (id && !seen.current.has(id)) {
        seen.current.add(id);
        entries.push(el as HTMLElement);
      }
    });

    if (entries.length === 0) return;

    const reduced = prefersReducedMotion();
    if (reduced) {
      gsap.set(entries, { opacity: 1, clearProps: "transform" });
      return;
    }

    // Batch: if multiple items arrive in the same tick, animate them together.
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      gsap.fromTo(
        entries,
        { opacity: 0, y: 12 },
        {
          opacity: 1,
          y: 0,
          duration: DUR_SLOW,
          ease: EASE_OUT,
          stagger: itemsStagger(entries.length),
          clearProps: "transform",
        },
      );
    }, 16);

    return () => {
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
    // Only re-scan when deps change — NOT on every render (streaming updates
    // text in place without adding new elements, so scanning is wasted work).
  }, [deps]); // eslint-disable-line react-hooks/exhaustive-deps

  return ref;
}

function itemsStagger(count: number): number {
  if (count <= 1) return 0;
  if (count <= 3) return 0.06;
  return 0.04;
}
