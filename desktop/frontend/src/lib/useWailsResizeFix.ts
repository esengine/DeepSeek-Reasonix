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
 * --- Maximised-window guard ---
 *
 * When the window is maximised, the viewport fills the entire work area so
 * the 6 px edge-detection zone coincides with the physical screen edges.
 * The old code would set a resize cursor (n-resize / ne-resize / e-resize)
 * that WebView2 never reliably reverted after `style.cursor = ""`,
 * leaving the cursor stuck in resize shape until the app was restarted.
 *
 * This hook now compares `window.outerWidth/outerHeight` with
 * `window.screen.availWidth/availHeight` synchronously on every mousemove.
 * When they match (within 5 px tolerance for DPI rounding), the window is
 * maximised and edge detection is skipped.  Any stale `flags.resizeEdge`
 * is cleared via `removeProperty("cursor")`, which forces WebView2 to
 * re-evaluate the cursor stack properly (unlike `style.cursor = ""` which
 * leaves a cursor:; declaration and does not trigger a SetCursor fallback).
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
      // When maximised the viewport fills the work area and the 6px edge
      // zone coincides with the physical screen edges.  Skip edge detection
      // so the cursor never shows a misleading resize shape.
      if (
        Math.abs(window.outerHeight - window.screen.availHeight) <= 5 &&
        Math.abs(window.outerWidth - window.screen.availWidth) <= 5
      ) {
        if (flags.resizeEdge !== undefined) {
          flags.resizeEdge = undefined;
          document.documentElement.style.removeProperty("cursor");
        }
        return;
      }

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
