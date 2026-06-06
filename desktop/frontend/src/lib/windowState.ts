// useWindowStatePersistence polls the Wails runtime for window geometry and
// persists it via SaveWindowState so the next launch restores the same size and
// position. No-op in browser dev (no window.runtime).
//
// The Go shutdown hook (app.saveWindowStateSync) provides the authoritative
// final save; this hook covers moves/resizes during the session.
//
// NOTE: navigator.sendBeacon or a sync XHR would let us block beforeunload,
// but Wails bindings are async (Go IPC). We accept that the very last resize
// event may not land; the 5s poll and the Go shutdown hook make this unlikely
// to matter.

import { useEffect, useRef } from "react";
import {
  WindowGetPosition,
  WindowGetSize,
  WindowIsMaximised,
} from "../../wailsjs/runtime/runtime";
import { app } from "./bridge";

export function useWindowStatePersistence() {
  const lastState = useRef("");

  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;

    let timer: ReturnType<typeof setInterval>;

    const save = async () => {
      try {
        const size = await WindowGetSize();
        const pos = await WindowGetPosition();
        const maximised = await WindowIsMaximised();
        const json = JSON.stringify({
          width: size.w,
          height: size.h,
          x: pos.x,
          y: pos.y,
          maximised,
        });
        if (json !== lastState.current) {
          await app.SaveWindowState({ width: size.w, height: size.h, x: pos.x, y: pos.y, maximised });
          lastState.current = json;
        }
      } catch {
        /* runtime not ready yet — poll will retry */
      }
    };

    // Debounced save on resize (500ms after the last resize event).
    let debounce: ReturnType<typeof setTimeout>;
    const onResize = () => {
      clearTimeout(debounce);
      debounce = setTimeout(save, 500);
    };
    window.addEventListener("resize", onResize);

    // Periodic poll every 5s for moves/maximise that don't trigger resize.
    timer = setInterval(save, 5000);

    // Best-effort save before the page unloads. The Go shutdown hook
    // (saveWindowStateSync) is the authoritative final persist.
    window.addEventListener("beforeunload", save);

    return () => {
      clearInterval(timer);
      clearTimeout(debounce);
      window.removeEventListener("resize", onResize);
      window.removeEventListener("beforeunload", save);
    };
  }, []);
}
