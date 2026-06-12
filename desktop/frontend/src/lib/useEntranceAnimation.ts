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
 *  - When `resetKey` changes (session switch), seen set + firstRun are
 *    cleared so the new session's first paint is also pre-seeded (no
 *    entrance animation for restored history).
 *
 * Usage:
 *   const entranceRef = useEntranceAnimation(items.length, sessionKey);
 */
export function useEntranceAnimation<T extends HTMLElement>(
  resetKey?: unknown,
  deps?: unknown,
  selector = "[data-entrance]",
) {
  const ref = useRef<T | null>(null);
  const seen = useRef(new Set<string>());
  const timerRef = useRef<number | null>(null);
  const firstRun = useRef(true);
  const prevResetKey = useRef(resetKey);

  // Reset on session switch.
  if (prevResetKey.current !== resetKey) {
    prevResetKey.current = resetKey;
    seen.current = new Set();
    firstRun.current = true;
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }

  // Single effect: on first mount, pre-seed the seen set (no animation).
  // On subsequent deps changes, animate only newly-added elements.
  // This avoids the double querySelectorAll that two separate effects cause.
  useEffect(() => {
    const container = ref.current;
    if (!container) return;

    const entries: HTMLElement[] = [];
    container.querySelectorAll(selector).forEach((el) => {
      const id = el.getAttribute("data-entrance");
      if (id && !seen.current.has(id)) {
        seen.current.add(id);
        // First run: just record IDs, don't animate history items.
        if (firstRun.current) return;
        entries.push(el as HTMLElement);
      }
    });

    if (firstRun.current) {
      firstRun.current = false;
      return; // Pre-seeded — no entrance animation for history items.
    }

    if (entries.length === 0) return;

    const reduced = prefersReducedMotion();
    if (reduced) {
      gsap.set(entries, { opacity: 1, clearProps: "transform" });
      return;
    }

    // Batch: if multiple items arrive in the same tick, animate together.
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
    // Only re-scan when deps change — NOT on every render.
  }, [deps]); // eslint-disable-line react-hooks/exhaustive-deps

  return ref;
}

function itemsStagger(count: number): number {
  if (count <= 1) return 0;
  if (count <= 3) return 0.06;
  return 0.04;
}
