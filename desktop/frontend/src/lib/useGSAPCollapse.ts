import { useLayoutEffect, useRef } from "react";
import { DUR_BASE, prefersReducedMotion } from "./gsapAnimations";

const CSS_EASE_OUT = "cubic-bezier(0.2, 0.72, 0.2, 1)";

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
  const animationRef = useRef<Animation | null>(null);
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
    const ease = opts?.ease && opts.ease !== "power2.out" ? opts.ease : CSS_EASE_OUT;
    animationRef.current?.cancel();
    animationRef.current = null;

    const finish = () => {
      el.style.height = open ? "auto" : "0px";
      if (open) onOpenRef.current?.();
      else onCloseRef.current?.();
    };
    if (reduced || typeof el.animate !== "function") {
      finish();
      return;
    }

    if (open) {
      const targetHeight = el.scrollHeight;
      const animation = el.animate(
        [{ height: "0px" }, { height: `${targetHeight}px` }],
        { duration: dur * 1000, easing: ease },
      );
      animationRef.current = animation;
      animation.onfinish = finish;
    } else {
      // Close: if caller provided a pre-swap height use it as the start,
      // otherwise measure the current (already-swapped) scrollHeight.
      const startHeight = opts?.prevHeight && opts.prevHeight > 0
        ? opts.prevHeight
        : Math.max(el.getBoundingClientRect().height, el.scrollHeight);
      const animation = el.animate(
        [{ height: `${startHeight}px` }, { height: "0px" }],
        { duration: dur * 1000, easing: ease },
      );
      animationRef.current = animation;
      animation.onfinish = finish;
    }
    return () => animationRef.current?.cancel();
  }, [open, ref]);
}
