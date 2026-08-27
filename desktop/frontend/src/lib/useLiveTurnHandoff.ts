import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ListItem } from "react-virtuoso";
import type { TranscriptRow } from "./transcriptRows";

const NO_HELD_ROWS: readonly TranscriptRow[] = [];

/** Keeps the completed live turn painted without contributing layout height. */
export function useLiveTurnHandoff({
  liveActive,
  liveRows,
  layoutSurfaceKey,
  isAtBottom,
  rows,
}: {
  liveActive: boolean;
  liveRows: readonly TranscriptRow[];
  layoutSurfaceKey: string;
  isAtBottom: boolean;
  rows: readonly TranscriptRow[];
}) {
  const heldRowsRef = useRef<readonly TranscriptRow[]>([]);
  const heldSurfaceRef = useRef(layoutSurfaceKey);
  const [holding, setHolding] = useState(false);
  const [mountRevision, setMountRevision] = useState(0);
  const tailMountedRef = useRef(false);
  const wasLiveActiveRef = useRef(false);

  if (liveActive) {
    wasLiveActiveRef.current = true;
    heldSurfaceRef.current = layoutSurfaceKey;
    heldRowsRef.current = liveRows;
    tailMountedRef.current = false;
    if (holding) setHolding(false);
  } else if (wasLiveActiveRef.current) {
    wasLiveActiveRef.current = false;
    if (heldSurfaceRef.current !== layoutSurfaceKey) heldRowsRef.current = [];
    if (heldRowsRef.current.length > 0 && isAtBottom && !holding) {
      tailMountedRef.current = false;
      setHolding(true);
    }
  }

  if (holding && heldRowsRef.current.length > 0) {
    const lastHeldKey = String(heldRowsRef.current[heldRowsRef.current.length - 1].key);
    if (!rows.some((row) => String(row.key) === lastHeldKey)) {
      heldRowsRef.current = [];
      tailMountedRef.current = false;
      setHolding(false);
    }
  }

  useEffect(() => {
    heldRowsRef.current = [];
    tailMountedRef.current = false;
    setHolding(false);
  }, [layoutSurfaceKey]);

  useEffect(() => {
    if (!holding) return;
    const timeout = window.setTimeout(() => {
      heldRowsRef.current = [];
      tailMountedRef.current = false;
      setHolding(false);
    }, 300);
    return () => window.clearTimeout(timeout);
  }, [holding]);

  const noteItemsRendered = useCallback((rendered: ListItem<TranscriptRow>[]) => {
    if (!holding) return;
    const held = heldRowsRef.current;
    const lastKey = held.length > 0 ? String(held[held.length - 1].key) : null;
    if (!tailMountedRef.current && (lastKey === null || rendered.some((item) => String(item.data?.key ?? "") === lastKey))) {
      tailMountedRef.current = true;
      setMountRevision((revision) => revision + 1);
    }
  }, [holding]);

  useLayoutEffect(() => {
    if (!holding || !tailMountedRef.current) return;
    heldRowsRef.current = [];
    tailMountedRef.current = false;
    setHolding(false);
  }, [holding, mountRevision]);

  const heldRows = heldSurfaceRef.current === layoutSurfaceKey ? heldRowsRef.current : NO_HELD_ROWS;
  return [heldRows, holding && heldRows.length > 0, noteItemsRendered] as const;
}
