import { useEffect, useMemo, useRef } from "react";
import { wrapToCells } from "../../../frame/width.js";

export interface WrapCache {
  text: string;
  lineCells: number;
  visualLines: string[];
  /** Invariant: equals wrapToCells(tailLogicalLine(text), lineCells).length. */
  tailVisualCount: number;
}

function wrapAll(text: string, lineCells: number): string[] {
  if (text.length === 0) return [""];
  return text.split("\n").flatMap((l) => wrapToCells(l, lineCells));
}

function tailLogicalLine(text: string): string {
  const i = text.lastIndexOf("\n");
  return i < 0 ? text : text.slice(i + 1);
}

export function wrapIncremental(
  text: string,
  lineCells: number,
  prev: WrapCache | null,
): WrapCache {
  const monotonic =
    prev !== null &&
    prev.lineCells === lineCells &&
    text.length >= prev.text.length &&
    text.startsWith(prev.text);

  if (!monotonic) {
    const visualLines = wrapAll(text, lineCells);
    const tailVisualCount = wrapToCells(tailLogicalLine(text), lineCells).length;
    return { text, lineCells, visualLines, tailVisualCount };
  }

  if (text.length === prev.text.length) return prev;

  const added = text.slice(prev.text.length);
  const prevTail = tailLogicalLine(prev.text);
  const prefixLen = prev.visualLines.length - prev.tailVisualCount;
  const prefix = prev.visualLines.slice(0, prefixLen);

  const nlIdx = added.indexOf("\n");
  if (nlIdx < 0) {
    const newTailVisual = wrapToCells(prevTail + added, lineCells);
    return {
      text,
      lineCells,
      visualLines: [...prefix, ...newTailVisual],
      tailVisualCount: newTailVisual.length,
    };
  }

  const finalizedLast = prevTail + added.slice(0, nlIdx);
  const finalizedWrap = wrapToCells(finalizedLast, lineCells);
  const remainder = added.slice(nlIdx + 1);
  const remainderLines = remainder.length === 0 ? [""] : remainder.split("\n");
  const newTailText = remainderLines[remainderLines.length - 1] ?? "";
  const newTailVisual = wrapToCells(newTailText, lineCells);
  const middleVisual = remainderLines.slice(0, -1).flatMap((l) => wrapToCells(l, lineCells));

  return {
    text,
    lineCells,
    visualLines: [...prefix, ...finalizedWrap, ...middleVisual, ...newTailVisual],
    tailVisualCount: newTailVisual.length,
  };
}

/** Debounce interval for lineCells changes — holds the previous width during
 * a terminal resize to avoid breaking monotonicity and triggering a full
 * re-wrap cascade while streaming tokens are still arriving. */
const LINE_CELLS_DEBOUNCE_MS = 120;

/** Streaming-aware wrap. Monotonic growth re-wraps only the tail logical line.
 * Terminal resize is debounced: the previous width is held briefly so streaming
 * content stays on the fast monotonic path, then snaps to the new width. */
export function useIncrementalWrap(text: string, lineCells: number): string[] {
  const cacheRef = useRef<WrapCache | null>(null);
  const stableCellsRef = useRef<number>(lineCells);
  const cellsTimerRef = useRef<NodeJS.Timeout | null>(null);

  // Debounce lineCells changes: during a resize, hold the old width briefly
  // so the incremental wrapper can stay in its fast monotonic path.
  const effectiveCells = useMemo(() => {
    if (stableCellsRef.current === lineCells) return lineCells;

    // First time we see a new width, schedule a commit after the debounce
    // window. Until then, keep using the old width.
    if (cellsTimerRef.current === null) {
      cellsTimerRef.current = setTimeout(() => {
        stableCellsRef.current = lineCells;
        cellsTimerRef.current = null;
      }, LINE_CELLS_DEBOUNCE_MS);
    }
    // If the width changes again before the timer fires, reset the timer
    // so we only commit once the width has been stable for the full window.
    else {
      clearTimeout(cellsTimerRef.current);
      cellsTimerRef.current = setTimeout(() => {
        stableCellsRef.current = lineCells;
        cellsTimerRef.current = null;
      }, LINE_CELLS_DEBOUNCE_MS);
      return stableCellsRef.current;
    }
    return stableCellsRef.current;
  }, [lineCells]);

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (cellsTimerRef.current !== null) {
        clearTimeout(cellsTimerRef.current);
        cellsTimerRef.current = null;
      }
    };
  }, []);

  return useMemo(() => {
    cacheRef.current = wrapIncremental(text, effectiveCells, cacheRef.current);
    return cacheRef.current.visualLines;
  }, [text, effectiveCells]);
}
