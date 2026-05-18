import { LogicalPosition, LogicalSize, getCurrentWindow } from "@tauri-apps/api/window";
import { useEffect } from "react";

const STORAGE_KEY = "reasonix.windowBounds";
const MIN_WIDTH = 400;
const MIN_HEIGHT = 300;
const SAVE_DEBOUNCE_MS = 300;

interface WindowBounds {
  width: number;
  height: number;
  x: number;
  y: number;
}

export function useWindowBounds() {
  useEffect(() => {
    const win = getCurrentWindow();

    // Restore saved bounds
    (async () => {
      try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return;
        const saved = JSON.parse(raw) as WindowBounds;
        if (saved.width >= MIN_WIDTH && saved.height >= MIN_HEIGHT) {
          await win.setSize(new LogicalSize(saved.width, saved.height));
          await win.setPosition(new LogicalPosition(saved.x, saved.y));
        }
      } catch {
        localStorage.removeItem(STORAGE_KEY);
      }
    })();

    // Debounced save on resize and move
    let timer: ReturnType<typeof setTimeout>;
    const scheduleSave = () => {
      clearTimeout(timer);
      timer = setTimeout(async () => {
        try {
          if (await win.isMaximized()) return;
          const bounds: WindowBounds = {
            width: window.innerWidth,
            height: window.innerHeight,
            x: window.screenX,
            y: window.screenY,
          };
          localStorage.setItem(STORAGE_KEY, JSON.stringify(bounds));
        } catch {
          /* silently give up — localStorage full or window closed */
        }
      }, SAVE_DEBOUNCE_MS);
    };

    let unlistenResize: (() => void) | undefined;
    let unlistenMove: (() => void) | undefined;
    win.listen("tauri://resize", scheduleSave).then((fn) => { unlistenResize = fn; });
    win.listen("tauri://move", scheduleSave).then((fn) => { unlistenMove = fn; });

    return () => {
      clearTimeout(timer);
      unlistenResize?.();
      unlistenMove?.();
    };
  }, []);
}
