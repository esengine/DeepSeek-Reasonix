// Shared timing configuration for CSS and Web Animations API transitions.
// The curves mirror the desktop CSS motion tokens.

/** 120ms — color/border hovers, tooltips. */
export const DUR_FAST = 0.12;

/** 180ms — popovers, menus, small enters. Matches CSS --dur-base. */
export const DUR_BASE = 0.18;

/** 340ms — drawers, modals, panel slides. Matches CSS --dur-slow. */
export const DUR_SLOW = 0.34;

/** Fast-out/decelerate curve used by expanding and entrance animations. */
export const CSS_EASE_OUT = "cubic-bezier(0.2, 0.72, 0.2, 1)";

/** Accelerating curve used by short exit animations. */
export const CSS_EASE_IN = "cubic-bezier(0.8, 0, 0.8, 0.28)";

/** Runs a short exit transition without letting cosmetic failures block work. */
export function animateElementExit(
  el: HTMLElement,
  options: { opacity: number; y: number; duration: number; onComplete: () => void },
) {
  let completed = false;
  const complete = () => {
    if (completed) return;
    completed = true;
    options.onComplete();
  };
  if (typeof el.animate !== "function") {
    complete();
    return;
  }
  let animation: Animation;
  try {
    animation = el.animate(
      [
        { opacity: 1, transform: "translateY(0)" },
        { opacity: options.opacity, transform: `translateY(${options.y}px)` },
      ],
      { duration: options.duration * 1000, easing: CSS_EASE_IN },
    );
  } catch {
    complete();
    return;
  }
  animation.onfinish = complete;
  animation.oncancel = complete;
}

/** Returns true when the user has requested reduced motion at the OS level. */
export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
