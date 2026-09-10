// useWindowStatePersistence polls the desktop host for window geometry and
// persists it via SaveWindowState so the next launch restores the same size and
// position. No-op in browser dev (no host).
//
// The frontend is the sole source of geometry: resize (debounced), a 5s poll,
// and beforeunload. Go never reads the native window geometry during
// beforeClose or shutdown (those paths can panic on Windows when DPI reports
// 0). The Go shutdown hook only re-persists the last frontend-reported state.

import { useEffect } from "react";
import { app } from "./bridge";
import { desktopHost } from "./desktopHost";

export interface WindowStateSnapshot {
  width: number;
  height: number;
  x: number;
  y: number;
  maximised: boolean;
}

interface WindowStateRuntime {
  getWindowBounds(): Promise<WindowStateSnapshot> | undefined;
}

// Serialize capture + persistence as one operation. Resize, polling, and
// beforeunload can all request a save while an earlier host call is still in
// flight; a queue prevents an older completion from overwriting a newer
// observation. A queued request captures geometry only when it reaches the
// front, so it observes the latest native state instead of replaying a stale
// snapshot.
export function createWindowStateSaver(
  runtime: WindowStateRuntime,
  persist: (state: WindowStateSnapshot) => Promise<void>,
): () => Promise<void> {
  let lastState = "";
  let queue = Promise.resolve();

  return () => {
    queue = queue.then(async () => {
      try {
        const bounds = await runtime.getWindowBounds();
        if (!bounds) return;
        const state = { width: bounds.width, height: bounds.height, x: bounds.x, y: bounds.y, maximised: bounds.maximised };
        const json = JSON.stringify(state);
        if (json === lastState) return;
        await persist(state);
        lastState = json;
      } catch {
        /* host not ready yet — a later request will retry */
      }
    });
    return queue;
  };
}

export function useWindowStatePersistence() {
  useEffect(() => {
    if (desktopHost().kind === "none") return;

    let timer: ReturnType<typeof setInterval>;
    const save = createWindowStateSaver(
      { getWindowBounds: () => desktopHost().native.getWindowBounds() },
      (state) => app.SaveWindowState(state),
    );

    // Debounced save on resize (500ms after the last resize event).
    let debounce: ReturnType<typeof setTimeout>;
    const onResize = () => {
      clearTimeout(debounce);
      debounce = setTimeout(save, 500);
    };
    window.addEventListener("resize", onResize);

    // Periodic poll every 5s for moves/maximise that don't trigger resize.
    timer = setInterval(save, 5000);

    // Best-effort save before the page unloads. Go re-persists the last
    // accepted report during shutdown without querying the native window.
    const onBeforeUnload = () => { void save(); };
    window.addEventListener("beforeunload", onBeforeUnload);

    return () => {
      clearInterval(timer);
      clearTimeout(debounce);
      window.removeEventListener("resize", onResize);
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
  }, []);
}

export function useViewportHeightVar() {
  useEffect(() => {
    if (typeof window === "undefined" || typeof document === "undefined") return;

    let frame = 0;
    const root = document.documentElement;
    const setHeight = () => {
      frame = 0;
      const height = Math.round(window.visualViewport?.height ?? window.innerHeight);
      if (height > 0) root.style.setProperty("--app-viewport-height", `${height}px`);
    };
    const schedule = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(setHeight);
    };

    schedule();
    window.addEventListener("resize", schedule);
    window.addEventListener("orientationchange", schedule);
    document.addEventListener("fullscreenchange", schedule);
    window.visualViewport?.addEventListener("resize", schedule);

    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("resize", schedule);
      window.removeEventListener("orientationchange", schedule);
      document.removeEventListener("fullscreenchange", schedule);
      window.visualViewport?.removeEventListener("resize", schedule);
    };
  }, []);
}
