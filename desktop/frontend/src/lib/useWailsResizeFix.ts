import { useEffect } from "react";

/**
 * WailsWailsFlags mirrors the `window.wails.flags` object injected by the
 * Wails v2 runtime (see internal/frontend/runtime/desktop/main.js).
 */
interface WailsFlags {
  enableResize: boolean;
  resizeEdge: string | undefined;
  borderThickness: number;
  defaultCursor: string | null;
  cssDragProperty: string;
  cssDragValue: string;
  cssDropProperty: string;
  cssDropValue: string;
  shouldDrag: boolean;
  deferDragToMouseMove: boolean;
  disableScrollbarDrag: boolean;
  disableDefaultContextMenu: boolean;
  enableWailsDragAndDrop: boolean;
}

interface WailsWindow {
  wails: {
    flags: WailsFlags;
    Callback: (msg: string) => void;
    EventsNotify: (msg: string) => void;
  };
}

declare global {
  interface Window {
    wails?: WailsWindow["wails"];
  }
}

/**
 * useWailsResizeFix
 *
 * Workaround for a Wails v2.12 frameless-window resize detection bug where
 * `window.outerWidth/outerHeight` (device-independent pixels) is compared
 * with `e.clientX/e.clientY` (CSS pixels).  When WebView2 ZoomFactor ≠ 1
 * these coordinate spaces diverge, causing the right/bottom resize zone to
 * extend far inward or disappear entirely.
 *
 * This hook disables the built-in mousemove handler (which uses the wrong
 * coordinate space) and replaces it with one that uses `window.innerWidth`
 * and `window.innerHeight` — both in CSS pixels, matching `e.clientX/Y`.
 *
 * The Wails mousedown handler still works because it only reads
 * `window.wails.flags.resizeEdge`, which we continue to set here.
 *
 * Upstream fix: https://github.com/wailsapp/wails/issues/4590 (Wails v3
 * sidestepped by clamping zoom ≥ 1.0).  Once Wails v2 ships a proper fix
 * this hook can be deleted.
 *
 * @example
 *   // In App.tsx or any component mounted for the app's lifetime:
 *   useWailsResizeFix(desktopPlatform === "windows");
 */
export function useWailsResizeFix(enabled: boolean): void {
  useEffect(() => {
    if (!enabled) return;
    const wails = window.wails;
    if (!wails) return; // not inside a Wails webview → no-op

    const flags = wails.flags;
    const bt = flags.borderThickness ?? 6;
    const previousEnableResize = flags.enableResize;
    const previousResizeEdge = flags.resizeEdge;
    const previousCursor = document.documentElement.style.cursor;

    // Restore the default cursor when we're done — memoize the initial value.
    const defaultCursor = previousCursor;

    const onMouseMove = (e: MouseEvent) => {
      // Both operands in CSS pixels — the bug fix.
      const iw = window.innerWidth;
      const ih = window.innerHeight;
      const cx = e.clientX;
      const cy = e.clientY;

      const right   = iw - cx < bt;
      const left    = cx < bt;
      const top     = cy < bt;
      const bottom  = ih - cy < bt;

      let edge: string | undefined;
      if      (right && bottom) edge = "se-resize";
      else if (left  && bottom) edge = "sw-resize";
      else if (right && top)    edge = "ne-resize";
      else if (left  && top)    edge = "nw-resize";
      else if (right)           edge = "e-resize";
      else if (left)            edge = "w-resize";
      else if (top)             edge = "n-resize";
      else if (bottom)          edge = "s-resize";

      if (edge !== flags.resizeEdge) {
        flags.resizeEdge = edge;
        document.documentElement.style.cursor = edge ?? (defaultCursor || "");
      }
    };

    // Disable Wails' built-in mousemove handler (the one that uses outerWidth).
    flags.enableResize = false;
    window.addEventListener("mousemove", onMouseMove);

    return () => {
      window.removeEventListener("mousemove", onMouseMove);
      flags.enableResize = previousEnableResize;
      flags.resizeEdge = previousResizeEdge;
      document.documentElement.style.cursor = previousCursor;
    };
  }, [enabled]);
}
