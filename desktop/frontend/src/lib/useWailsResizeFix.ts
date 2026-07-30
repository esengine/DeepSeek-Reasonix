import { useEffect } from "react";

const RESIZE_CURSORS = new Set([
  "e-resize",
  "n-resize",
  "ne-resize",
  "nw-resize",
  "s-resize",
  "se-resize",
  "sw-resize",
  "w-resize",
]);

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
 * Maximised state cannot be inferred safely from viewport dimensions: a
 * manually sized or FancyZones-managed window can also fill the work area.
 * Instead, this hook consumes the native maximise state shared with the Windows
 * titlebar controls. Mousemove stays synchronous and never performs IPC.
 *
 * When maximised, edge detection is skipped and any stale resize cursor is
 * replaced with an explicit default cursor. This also covers startup restores
 * where the native cursor is already stuck but Wails' `resizeEdge` flag was
 * never populated.
 *
 * Upstream fix: https://github.com/wailsapp/wails/issues/4590 (Wails v3
 * sidestepped by clamping zoom ≥ 1.0).  Once Wails v2 ships a proper fix
 * this hook can be deleted.
 *
 * @example
 *   // In App.tsx or any component mounted for the app's lifetime:
 *   useWailsResizeFix(desktopPlatform === "windows");
 */
export function useWailsResizeFix(enabled: boolean, maximised = false): void {
  useEffect(() => {
    if (!enabled) return;
    const wails = window.wails;
    if (!wails) return; // not inside a Wails webview → no-op

    const flags = wails.flags;
    const bt = flags.borderThickness ?? 6;
    const previousEnableResize = flags.enableResize;
    const previousResizeEdge = flags.resizeEdge;
    const previousCursor = document.documentElement.style.cursor;

    // Prefer Wails' remembered cursor. A resize-shaped inline cursor at mount
    // is stale state, not the application's default.
    const rememberedCursor = flags.defaultCursor ?? previousCursor;
    const defaultCursor = RESIZE_CURSORS.has(rememberedCursor) ? "" : rememberedCursor;
    const restoredCursor = defaultCursor || "default";

    const clearResizeState = () => {
      flags.resizeEdge = undefined;
      if (document.documentElement.style.cursor !== restoredCursor) {
        document.documentElement.style.cursor = restoredCursor;
      }
    };

    // Normalise stale startup state immediately, before the first native state
    // query completes. A normal window will restore the correct edge on its next
    // mousemove; a maximised window keeps the default cursor.
    if (maximised || previousResizeEdge !== undefined || RESIZE_CURSORS.has(previousCursor)) {
      clearResizeState();
    }

    const onMouseMove = (e: MouseEvent) => {
      if (maximised) {
        clearResizeState();
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
        document.documentElement.style.cursor = edge ?? restoredCursor;
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
  }, [enabled, maximised]);
}
