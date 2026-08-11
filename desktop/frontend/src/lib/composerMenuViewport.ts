const COMPOSER_MENU_GAP = 6;
const VIEWPORT_EDGE_GAP = 8;

export const COMPOSER_MENU_AVAILABLE_HEIGHT = "--composer-menu-available-height";

export function composerMenuAvailableHeight(anchorTop: number): number {
  if (!Number.isFinite(anchorTop)) return 0;
  return Math.max(0, Math.floor(anchorTop - COMPOSER_MENU_GAP - VIEWPORT_EDGE_GAP));
}

function updateAvailableHeight(anchor: HTMLElement): void {
  const next = `${composerMenuAvailableHeight(anchor.getBoundingClientRect().top)}px`;
  if (anchor.style.getPropertyValue(COMPOSER_MENU_AVAILABLE_HEIGHT) !== next) {
    anchor.style.setProperty(COMPOSER_MENU_AVAILABLE_HEIGHT, next);
  }
}

// The composer menu is absolutely positioned above its anchor, so viewport
// height alone cannot describe the space available to it. Measure the anchor's
// real top edge and observe the surrounding layout: terminal resizing moves the
// composer without necessarily resizing the composer itself.
export function observeComposerMenuViewport(anchor: HTMLElement): () => void {
  let live = true;
  let frame: number | null = null;

  const update = () => {
    if (!live || !anchor.isConnected) return;
    updateAvailableHeight(anchor);
  };
  const scheduleUpdate = () => {
    if (!live || frame !== null) return;
    frame = window.requestAnimationFrame(() => {
      frame = null;
      update();
    });
  };

  // useLayoutEffect calls this before paint, so measure synchronously to avoid
  // one frame at the 50vh CSS fallback when the terminal leaves less room.
  update();

  const observer = typeof ResizeObserver === "undefined"
    ? null
    : new ResizeObserver(scheduleUpdate);
  if (observer) {
    const observed = new Set<Element>();
    let current: HTMLElement | null = anchor;
    while (current) {
      if (!observed.has(current)) {
        observer.observe(current);
        observed.add(current);
      }
      // Sibling size changes can move the anchor without changing its own box
      // (the terminal drawer is the important example), so observe each
      // ancestor's direct children as well as the ancestor itself.
      for (const child of Array.from(current.children)) {
        if (child instanceof HTMLElement && !observed.has(child)) {
          observer.observe(child);
          observed.add(child);
        }
      }
      current = current.parentElement;
    }
  }

  window.addEventListener("resize", scheduleUpdate);
  window.addEventListener("scroll", scheduleUpdate, true);
  window.visualViewport?.addEventListener("resize", scheduleUpdate);
  window.visualViewport?.addEventListener("scroll", scheduleUpdate);

  return () => {
    live = false;
    if (frame !== null) window.cancelAnimationFrame(frame);
    observer?.disconnect();
    window.removeEventListener("resize", scheduleUpdate);
    window.removeEventListener("scroll", scheduleUpdate, true);
    window.visualViewport?.removeEventListener("resize", scheduleUpdate);
    window.visualViewport?.removeEventListener("scroll", scheduleUpdate);
    anchor.style.removeProperty(COMPOSER_MENU_AVAILABLE_HEIGHT);
  };
}
