import { useEffect, useMemo, useRef } from "react";
import { CSS_EASE_OUT, DUR_SLOW, prefersReducedMotion } from "./motion";

// Animates each data-entrance element in once. First mount (and every
// resetKey change) pre-seeds the seen set so restored history never animates;
// the scan only runs when deps changes, skipping streaming token updates.
export function useEntranceAnimation<T extends HTMLElement>(
  resetKey?: unknown,
  deps?: unknown,
  selector = "[data-entrance]",
  seedIds: readonly string[] = [],
) {
  const ref = useRef<T | null>(null);
  // Virtualized history rows may mount after the first DOM scan. Seed their
  // model IDs up front so a later append never mistakes restored rows for new
  // content and animates the whole viewport.
  const seen = useRef(new Set<string>());
  const seeded = useRef(false);
  if (!seeded.current) {
    seen.current = new Set(seedIds);
    seeded.current = true;
  }
  const timerRef = useRef<number | null>(null);
  const timerAnimations = useRef<Animation[]>([]);
  const firstRun = useRef(true);
  const prevResetKey = useRef(resetKey);

  // Reset on session switch.
  if (prevResetKey.current !== resetKey) {
    prevResetKey.current = resetKey;
    seen.current = new Set(seedIds);
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
      for (const entry of entries) {
        entry.style.opacity = "1";
        entry.style.transform = "";
      }
      return;
    }

    // Batch: if multiple items arrive in the same tick, animate together.
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      const animations = entries.map((entry, index) => {
        const settle = () => {
          entry.style.opacity = "1";
          entry.style.transform = "";
        };
        if (typeof entry.animate !== "function") {
          settle();
          return null;
        }
        let animation: Animation;
        try {
          animation = entry.animate(
            [
              { opacity: 0, transform: "translateY(12px)" },
              { opacity: 1, transform: "translateY(0)" },
            ],
            {
              duration: DUR_SLOW * 1000,
              easing: CSS_EASE_OUT,
              delay: index * itemsStagger(entries.length) * 1000,
            },
          );
        } catch {
          // Entrance motion is cosmetic. Keep later entries running and expose
          // this entry immediately if a WebView rejects the animation.
          settle();
          return null;
        }
        animation.onfinish = settle;
        animation.oncancel = settle;
        return animation;
      });
      timerAnimations.current = animations.filter((animation): animation is Animation => animation !== null);
    }, 16);

    return () => {
      if (timerRef.current !== null) clearTimeout(timerRef.current);
      for (const animation of timerAnimations.current) {
        try {
          animation.cancel();
        } catch {
          // Cancellation is cleanup-only; each entry already has a final style.
        }
      }
      timerAnimations.current = [];
    };
    // Only re-scan when deps change — NOT on every render.
  }, [deps]); // eslint-disable-line react-hooks/exhaustive-deps

  return ref;
}

export function useTranscriptEntranceAnimation<T extends HTMLElement>(
  tabId: string | undefined,
  revealSignal: unknown,
  items: readonly { id: string }[],
) {
  const seedIds = useMemo(() => items.map((item) => item.id), [items]);
  // A tail append preserves this key; a surface switch, reveal, or history
  // prepend resets it and pre-seeds every model ID before virtual rows mount.
  const resetKey = `${tabId ?? ""}|${String(revealSignal)}|${items[0]?.id ?? ""}`;
  return useEntranceAnimation<T>(resetKey, items.length, "[data-entrance]", seedIds);
}

function itemsStagger(count: number): number {
  if (count <= 1) return 0;
  if (count <= 3) return 0.06;
  return 0.04;
}
