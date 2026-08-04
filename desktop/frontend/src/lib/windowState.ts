// useWindowStatePersistence polls the Wails runtime for window geometry and
// persists it via SaveWindowState so the next launch restores the same size and
// position. No-op in browser dev (no window.runtime).
//
// The frontend is the sole source of geometry: resize (debounced), a 5s poll,
// and beforeunload. Go never reads WindowGetSize/Position/IsMaximised during
// beforeClose or shutdown (those paths can panic on Windows when DPI reports
// 0). The Go shutdown hook only re-persists the last frontend-reported state.

import { useEffect } from "react";
import { app } from "./bridge";

export interface WindowStateSnapshot {
  width: number;
  height: number;
  x: number;
  y: number;
  maximised: boolean;
}

interface WindowStateRuntime {
  WindowGetSize(): Promise<{ w: number; h: number }>;
  WindowGetPosition(): Promise<{ x: number; y: number }>;
  WindowIsMaximised(): Promise<boolean>;
}

// Serialize capture + persistence as one operation. Resize, polling, and
// beforeunload can all request a save while an earlier Wails call is still in
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
        const size = await runtime.WindowGetSize();
        const pos = await runtime.WindowGetPosition();
        const maximised = await runtime.WindowIsMaximised();
        const state = { width: size.w, height: size.h, x: pos.x, y: pos.y, maximised };
        const json = JSON.stringify(state);
        if (json === lastState) return;
        await persist(state);
        lastState = json;
      } catch {
        /* runtime not ready yet — a later request will retry */
      }
    });
    return queue;
  };
}

export function useWindowStatePersistence() {
  useEffect(() => {
    const runtime = typeof window !== "undefined" ? window.runtime : undefined;
    if (!runtime?.WindowGetSize || !runtime.WindowGetPosition || !runtime.WindowIsMaximised) return;
    const { WindowGetSize, WindowGetPosition, WindowIsMaximised } = runtime;

    let timer: ReturnType<typeof setInterval>;
    const save = createWindowStateSaver(
      { WindowGetSize, WindowGetPosition, WindowIsMaximised },
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
