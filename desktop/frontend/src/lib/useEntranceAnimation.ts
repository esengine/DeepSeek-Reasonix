import { useEffect, useRef } from "react";
import gsap from "gsap";
import { DUR_SLOW, EASE_OUT, prefersReducedMotion } from "./gsapAnimations";

/**
 * useEntranceAnimation — animates newly-mounted elements as they appear in
 * the DOM. Tracks seen item IDs so each element animates in only once.
 *
 * Usage in a rendering loop:
 *   const entrance = useEntranceAnimation<HTMLDivElement>();
 *   // ... in JSX:
 *   <div ref={entrance.ref}>
 *     {items.map((it) => (
 *       <div key={it.id} data-entrance={it.id}>
 *         ...
 *       </div>
 *     ))}
 *   </div>
 *
 * When a new element with data-entrance appears, it fades+slides in.
 * */
export function useEntranceAnimation<T extends HTMLElement>(
  selector = "[data-entrance]",
) {
  const ref = useRef<T | null>(null);
  const seen = useRef(new Set<string>());
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    const container = ref.current;
    if (!container) return;

    // Walk the container for elements with data-entrance that we haven't
    // seen yet.
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
      // Reduced motion: just show instantly.
      gsap.set(entries, { opacity: 1, clearProps: "transform" });
      return;
    }

    // Debounce: if items arrive in a batch (e.g. initial load or multiple
    // tool calls in the same turn), collect them and animate together.
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
  });

  return ref;
}

/** Choose a stagger delay based on how many elements arrived together. */
function itemsStagger(count: number): number {
  if (count <= 1) return 0;
  if (count <= 3) return 0.06;
  return 0.04;
}
