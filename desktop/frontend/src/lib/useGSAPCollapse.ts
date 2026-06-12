import { useLayoutEffect, useRef } from "react";
import gsap from "gsap";
import { DUR_BASE, EASE_OUT, prefersReducedMotion } from "./gsapAnimations";

/**
 * useGSAPCollapse — animate a container's height between 0 and its
 * scrollHeight whenever `open` flips.  Replaces the old CSS max-height
 * hack with a precise pixel-level GSAP tween.
 *
 * Usage:
 *   const ref = useRef<HTMLDivElement>(null);
 *   useGSAPCollapse(ref, open);
 *   return <div ref={ref}>{children}</div>;
 *
 * The container should have `overflow: hidden` in CSS.  No extra wrapper
 * elements needed.
 */
export function useGSAPCollapse(
  ref: React.RefObject<HTMLElement | null>,
  open: boolean,
  opts?: {
    duration?: number;
    ease?: string;
    /** Called after the open animation completes. */
    onOpenComplete?: () => void;
    /** Called after the close animation completes. */
    onCloseComplete?: () => void;
    /** When closing, use this height as the starting point instead of
     *  measuring scrollHeight (which may have already shrunk due to
     *  content being conditionally removed). */
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

    // Skip the very first render — we don't want to animate from 0→auto
    // on mount.  Use a direct style write (no GSAP overhead) for the
    // initial state so 400+ collapsed items don't each go through
    // gsap.set property resolution.
    if (prevOpen.current === null) {
      prevOpen.current = open;
      el.style.height = open ? "auto" : "0px";
      return;
    }

    // No change — nothing to do.
    if (prevOpen.current === open) return;
    prevOpen.current = open;

    const reduced = prefersReducedMotion();
    const dur = reduced ? 0.001 : (opts?.duration ?? DUR_BASE);
    const ease = opts?.ease ?? EASE_OUT;

    // Kill any in-flight GSAP animations on this element so we always
    // start from the current rendered height.
    gsap.killTweensOf(el);

    if (open) {
      // Phase 1 — measure the target (auto) height without visible change.
      gsap.set(el, { height: "auto" });
      const targetHeight = el.scrollHeight;
      // Phase 2 — animate from current (which is 0 or whatever the kill
      // left us at) to the measured target height; then clear the inline
      // style so the element returns to `height: auto` / CSS-driven flow.
      gsap.fromTo(
        el,
        { height: 0 },
        {
          height: targetHeight,
          duration: dur,
          ease,
          clearProps: "height",
          onComplete: () => onOpenRef.current?.(),
        },
      );
    } else {
      // Close: if caller provided a pre-swap height use it as the start,
      // otherwise measure the current (already-swapped) scrollHeight.
      const startHeight = opts?.prevHeight && opts.prevHeight > 0
        ? opts.prevHeight
        : (gsap.set(el, { height: "auto" }), el.scrollHeight);
      gsap.fromTo(
        el,
        { height: startHeight },
        {
          height: 0,
          duration: dur,
          ease,
          onComplete: () => onCloseRef.current?.(),
        },
      );
    }
  }, [open, ref]);
}
