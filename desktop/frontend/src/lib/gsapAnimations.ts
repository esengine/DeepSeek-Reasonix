// Shared GSAP animation configuration.
// Mirrors the CSS token system (--dur-fast/--dur-base/--dur-slow/--ease-out)
// so JS-driven animations stay in sync with the CSS transition layer.

/** 120ms — color/border hovers, tooltips. */
export const DUR_FAST = 0.12;

/** 180ms — popovers, menus, small enters. Matches CSS --dur-base. */
export const DUR_BASE = 0.18;

/** 340ms — drawers, modals, panel slides. Matches CSS --dur-slow. */
export const DUR_SLOW = 0.34;

/** "power2.out" approximates the app-wide CSS cubic-bezier(0.2, 0.72, 0.2, 1). */
export const EASE_OUT = "power2.out";

/**
 * Map GSAP-style easing names to CSS easing strings accepted by the Web
 * Animations API (`Element.animate`). GSAP easing names like "power2.in"
 * are NOT valid CSS easing values — passing one to `el.animate` throws a
 * TypeError (`'power2.in' is not a valid value for easing`). Any code that
 * feeds a user-facing easing string into WAAPI must go through this first.
 */
export function toCssEasing(ease: string): string {
  switch (ease) {
    case EASE_OUT: // "power2.out"
      return "cubic-bezier(0.2, 0.72, 0.2, 1)";
    case "power2.in":
      // GSAP power2.in (accelerating) cubic-bezier approximation.
      return "cubic-bezier(0.55, 0.06, 0.68, 0.19)";
    default:
      // Already a CSS easing value (or unknown) — pass through unchanged.
      return ease;
  }
}

/** Returns true when the user has requested reduced motion at the OS level. */
export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
