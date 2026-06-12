// Shared GSAP animation configuration.
// Mirrors the CSS token system (--dur-fast/--dur-base/--dur-slow/--ease-out)
// so JS-driven animations stay in sync with the CSS transition layer.

/** 120ms — color/border hovers, tooltips. */
export const DUR_FAST = 0.12;

/** 180ms — popovers, menus, small enters. Matches CSS --dur-base. */
export const DUR_BASE = 0.18;

/** 340ms — drawers, modals, panel slides. Matches CSS --dur-slow. */
export const DUR_SLOW = 0.34;

/** 420ms — large overlay fades. Matches CSS --dur-slower. */
export const DUR_SLOWER = 0.42;

/**
 * "power2.out" approximates the CSS `cubic-bezier(0.2, 0.72, 0.2, 1)`
 * used across the app. GSAP's power2.out is a canonical fast-out ease.
 */
export const EASE_OUT = "power2.out";

/** Symmetric ease for entrances that should also decelerate out. */
export const EASE_IN_OUT = "power2.inOut";

/** Stronger deceleration for scroll / layout transitions. */
export const EASE_SCROLL = "power3.out";

/** Returns true when the user has requested reduced motion at the OS level. */
export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
