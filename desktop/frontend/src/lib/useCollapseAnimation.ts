import { useLayoutEffect, useRef } from "react";
import { CSS_EASE_OUT, DUR_BASE, prefersReducedMotion } from "./motion";

/**
 * Animates a container's height between 0 and its scrollHeight whenever
 * `open` flips. The visual transition always fails open: an unavailable or
 * rejecting Web Animations implementation must still apply the requested
 * final state and completion callback.
 */
export function useCollapseAnimation(
  ref: React.RefObject<HTMLElement | null>,
  open: boolean,
  opts?: {
    duration?: number;
    /** Called after the open animation completes or falls back. */
    onOpenComplete?: () => void;
    /** Called after the close animation completes or falls back. */
    onCloseComplete?: () => void;
    /** When closing, use this height as the starting point instead of
     * measuring scrollHeight after content has changed. */
    prevHeight?: number;
  },
) {
  const prevOpen = useRef<boolean | null>(null);
  const onOpenRef = useRef(opts?.onOpenComplete);
  const onCloseRef = useRef(opts?.onCloseComplete);
  onOpenRef.current = opts?.onOpenComplete;
  onCloseRef.current = opts?.onCloseComplete;

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;

    // Initial history can contain hundreds of collapsed rows. Seed the final
    // state directly before paint instead of constructing animations on mount.
    if (prevOpen.current === null) {
      prevOpen.current = open;
      el.style.height = open ? "auto" : "0px";
      return;
    }

    if (prevOpen.current === open) return;
    prevOpen.current = open;

    const configuredDuration = opts?.duration ?? DUR_BASE;
    const duration = prefersReducedMotion()
      ? 0
      : (Number.isFinite(configuredDuration) && configuredDuration >= 0 ? configuredDuration : DUR_BASE) * 1000;
    let superseded = false;
    let settled = false;
    const finish = () => {
      if (superseded || settled) return;
      settled = true;
      el.style.height = open ? "auto" : "0px";
      if (open) onOpenRef.current?.();
      else onCloseRef.current?.();
    };

    if (duration === 0 || typeof el.animate !== "function") {
      finish();
      return;
    }

    const keyframes = open
      ? [{ height: "0px" }, { height: `${el.scrollHeight}px` }]
      : [
          {
            height: `${opts?.prevHeight && opts.prevHeight > 0
              ? opts.prevHeight
              : Math.max(el.getBoundingClientRect().height, el.scrollHeight)}px`,
          },
          { height: "0px" },
        ];

    let animation: Animation;
    try {
      animation = el.animate(keyframes, { duration, easing: CSS_EASE_OUT });
    } catch {
      finish();
      return;
    }

    animation.onfinish = finish;
    animation.oncancel = finish;
    return () => {
      // A dependency change supersedes this direction. Suppress the old final
      // state/callback before cancellation so the next effect owns settlement.
      superseded = true;
      try {
        animation.cancel();
      } catch {
        // Cancellation is cleanup-only; the next render already owns state.
      }
    };
  }, [open, ref]);
}
