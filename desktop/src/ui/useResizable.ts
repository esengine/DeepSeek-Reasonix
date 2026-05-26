import { useCallback, useEffect, useRef, useState } from "react";
import { getThreadMaxWidth } from "./thread-layout";

const MIN_WIDTH = 160;
const MAX_WIDTH_PCT = 0.4;
const CSS_VAR = { side: "--side-width", ctx: "--ctx-width" } as const;
const PERSIST_KEY_SIDE = "reasonix.sideWidth";
const PERSIST_KEY_CTX = "reasonix.ctxWidth";

export function useResizable(
  side: "side" | "ctx",
  collapsed: boolean,
): {
  width: number;
  onMouseDown: (e: React.MouseEvent) => void;
} {
  const persistKey = side === "side" ? PERSIST_KEY_SIDE : PERSIST_KEY_CTX;
  const defaultWidth = side === "side" ? 244 : 320;

  const [width, setWidth] = useState(() => {
    try {
      const saved = localStorage.getItem(persistKey);
      if (saved) {
        const n = Number(saved);
        if (Number.isFinite(n) && n >= MIN_WIDTH) return n;
      }
    } catch {
      /* localStorage not available */
    }
    return defaultWidth;
  });

  const draggingRef = useRef(false);
  const startXRef = useRef(0);
  const startWidthRef = useRef(0);
  const widthRef = useRef(width);
  widthRef.current = width;
  const cssVar = CSS_VAR[side];
  const appRef = useRef<HTMLElement | null>(null);

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    draggingRef.current = true;
    startXRef.current = e.clientX;
    startWidthRef.current = widthRef.current;
    const appEl = document.querySelector(".app") as HTMLElement | null;
    appRef.current = appEl;
    if (appEl) appEl.dataset.dragging = "true";
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  }, []);

  useEffect(() => {
    if (collapsed) return;

    const onMove = (e: MouseEvent) => {
      if (!draggingRef.current) return;
      const appEl = appRef.current;
      if (!appEl) return;

      const delta = e.clientX - startXRef.current;
      let next: number;
      if (side === "side") {
        next = startWidthRef.current + delta;
      } else {
        next = startWidthRef.current - delta;
      }
      const maxW = Math.floor(window.innerWidth * MAX_WIDTH_PCT);
      next = Math.max(MIN_WIDTH, Math.min(next, maxW));
      widthRef.current = next;

      appEl.style.setProperty(cssVar, `${next}px`);
      setWidth(next);

      const otherVar = side === "side" ? "--ctx-width" : "--side-width";
      const otherW = Number.parseFloat(appEl.style.getPropertyValue(otherVar)) || 0;
      const tMax = getThreadMaxWidth({
        viewportWidth: window.innerWidth,
        visibleSide: side === "side" ? next : otherW,
        visibleCtx: side === "ctx" ? next : otherW,
      });
      appEl.style.setProperty("--thread-max-width", `${tMax}px`);
      appEl.style.setProperty("--composer-max-width", `${tMax}px`);
    };

    const onUp = () => {
      if (!draggingRef.current) return;
      draggingRef.current = false;
      const appEl = appRef.current;
      if (appEl) delete appEl.dataset.dragging;
      appRef.current = null;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      try {
        localStorage.setItem(persistKey, String(widthRef.current));
      } catch {
        /* localStorage not available */
      }
    };

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [collapsed, side, persistKey, cssVar]);

  return { width, onMouseDown };
}
