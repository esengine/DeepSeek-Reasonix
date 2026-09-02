import { useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { transcriptRowLayoutVariant } from "./transcriptRowGeometry";
import { transcriptRowMeasurementVersion, type TranscriptRow, type TurnModel } from "./transcriptRows";
import { resolveLiveTurnGrowthFloor, splitTranscriptLiveRows } from "./transcriptLiveTurn";

export function useTranscriptLiveTurnStability({
  turnModels,
  rows,
  liveId,
  running,
  stabilityKey,
  scrollElement,
  hydrating,
  tailOwnedRef,
  pinLiveTailBeforePaint,
}: {
  turnModels: readonly TurnModel[];
  rows: readonly TranscriptRow[];
  liveId: string | undefined;
  running: boolean;
  stabilityKey: string;
  scrollElement: HTMLElement | null;
  hydrating: boolean;
  tailOwnedRef: RefObject<boolean>;
  pinLiveTailBeforePaint: () => boolean;
}) {
  const liveSplit = useMemo(
    () => splitTranscriptLiveRows(turnModels, rows, liveId, running),
    [turnModels, rows, liveId, running],
  );
  const liveGeometryRevision = useMemo(
    () => liveSplit.liveRows.map((row) => [
      String(row.key),
      row.kind,
      transcriptRowLayoutVariant(row),
      transcriptRowMeasurementVersion(row),
    ].join("\u0000")).join("\u0001"),
    [liveSplit.liveRows],
  );
  const previousRowCountRef = useRef(liveSplit.liveRows.length);
  const previousHeightRef = useRef(0);
  const stabilityEpochRef = useRef(stabilityKey);
  const epochMatches = stabilityEpochRef.current === stabilityKey;
  const [heldMinHeight, setHeldMinHeight] = useState<number | null>(null);
  const heldMinHeightRef = useRef(heldMinHeight);
  const activeHeldMinHeight = epochMatches ? heldMinHeight : null;
  heldMinHeightRef.current = activeHeldMinHeight;
  const liveMinHeight = epochMatches
    ? resolveLiveTurnGrowthFloor(
      previousRowCountRef.current,
      liveSplit.liveRows.length,
      previousHeightRef.current,
      activeHeldMinHeight,
    )
    : null;

  // The answer/tool boundary must repair an already-owned tail in the same
  // commit, before Virtuoso publishes its asynchronous Footer measurement.
  useLayoutEffect(() => {
    if (stabilityEpochRef.current !== stabilityKey) {
      stabilityEpochRef.current = stabilityKey;
      previousHeightRef.current = 0;
      if (heldMinHeight !== null) setHeldMinHeight(null);
    }
    if (!liveSplit.liveActive) {
      previousRowCountRef.current = 0;
      previousHeightRef.current = 0;
      if (heldMinHeight !== null) setHeldMinHeight(null);
      return;
    }
    const region = scrollElement?.querySelector<HTMLElement>(".transcript__live-region");
    const content = region?.querySelector<HTMLElement>(".transcript__live-content");
    const floor = resolveLiveTurnGrowthFloor(
      previousRowCountRef.current,
      liveSplit.liveRows.length,
      previousHeightRef.current,
      activeHeldMinHeight,
    );
    previousRowCountRef.current = liveSplit.liveRows.length;
    if (region && content) {
      const naturalHeight = content.getBoundingClientRect().height;
      if (floor !== null && naturalHeight + 0.5 < floor) {
        if (activeHeldMinHeight !== floor) setHeldMinHeight(floor);
        previousHeightRef.current = floor;
      } else {
        if (activeHeldMinHeight !== null && naturalHeight + 0.5 >= activeHeldMinHeight) setHeldMinHeight(null);
        previousHeightRef.current = Math.max(naturalHeight, region.getBoundingClientRect().height);
      }
    }
    if (!hydrating && tailOwnedRef.current) pinLiveTailBeforePaint();
  }, [heldMinHeight, hydrating, liveGeometryRevision, liveSplit.liveActive, liveSplit.liveRows, pinLiveTailBeforePaint, scrollElement, stabilityKey, tailOwnedRef]);

  useLayoutEffect(() => {
    const content = scrollElement?.querySelector<HTMLElement>(".transcript__live-content");
    if (!content || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (stabilityEpochRef.current !== stabilityKey) return;
      const naturalHeight = content.getBoundingClientRect().height;
      const held = heldMinHeightRef.current;
      if (held !== null && naturalHeight + 0.5 >= held) setHeldMinHeight(null);
      if (held === null) previousHeightRef.current = naturalHeight;
    });
    observer.observe(content);
    return () => observer.disconnect();
  }, [liveGeometryRevision, scrollElement, stabilityKey]);

  return { liveSplit, liveMinHeight };
}
