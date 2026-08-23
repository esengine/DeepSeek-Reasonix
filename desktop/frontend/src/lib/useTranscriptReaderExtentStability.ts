import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react";
import {
  createTranscriptReaderExtentGuard,
  observeTranscriptReaderExtent,
  resolveTranscriptReaderExtentCorrection,
  transcriptReaderExtentCanCorrect,
  transcriptReaderExtentHasCollapsed,
  type TranscriptReaderExtentGuard,
} from "./transcriptReaderExtentStability";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom } from "./transcriptScrollGeometry";
import type { TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
import { captureTranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";

const READER_EXTENT_STABILITY_MS = 180;

type ActiveReaderExtentGuard = TranscriptReaderExtentGuard & {
  element: HTMLDivElement;
  generation: number;
  deadline: number;
  frame: number | null;
};

export function useTranscriptReaderExtentStability({
  generationRef,
  modeRef,
  scrollRef,
  writeCorrection,
}: {
  generationRef: RefObject<number>;
  modeRef: RefObject<TranscriptScrollMode>;
  scrollRef: RefObject<HTMLDivElement | null>;
  writeCorrection: (write: TranscriptScrollWriteRecord) => boolean;
}) {
  const guardRef = useRef<ActiveReaderExtentGuard | null>(null);

  const cancel = useCallback(() => {
    const guard = guardRef.current;
    guardRef.current = null;
    if (guard?.frame != null) cancelAnimationFrame(guard.frame);
  }, []);

  // While a guard is armed it owns post-gesture extent corrections; the
  // steady-state anchor compensation must stay out of its way.
  const isActive = useCallback(() => guardRef.current !== null, []);

  const observe = useCallback((element = scrollRef.current) => {
    const guard = guardRef.current;
    if (!element || guard?.element !== element) return false;
    observeTranscriptReaderExtent(guard, element);
    return transcriptReaderExtentHasCollapsed(guard);
  }, [scrollRef]);

  const arm = useCallback((deltaY: number) => {
    cancel();
    const element = scrollRef.current;
    if (!element) return;
    const guard = createTranscriptReaderExtentGuard(
      element,
      captureTranscriptLayoutAnchor(element, false),
      deltaY,
    );
    if (!guard) return;
    const active: ActiveReaderExtentGuard = {
      ...guard,
      element,
      generation: generationRef.current,
      deadline: Date.now() + READER_EXTENT_STABILITY_MS,
      frame: null,
    };
    const tick = () => {
      active.frame = null;
      if (
        guardRef.current !== active
        || generationRef.current !== active.generation
        || scrollRef.current !== active.element
        || modeRef.current !== "manual"
      ) {
        if (guardRef.current === active) guardRef.current = null;
        return;
      }
      const snapshot = {
        scrollTop: element.scrollTop,
        scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight,
      };
      observeTranscriptReaderExtent(active, snapshot);
      if (transcriptReaderExtentCanCorrect(active, snapshot)) {
        const row = active.anchor
          ? Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
            .find((candidate) => candidate.dataset.rowKey === active.anchor?.rowKey)
          : undefined;
        const currentAnchorOffset = row
          ? row.getBoundingClientRect().top - element.getBoundingClientRect().top
          : undefined;
        const correction = resolveTranscriptReaderExtentCorrection(active, snapshot, currentAnchorOffset);
        if (correction !== undefined && writeCorrection({
            owner: "reader-stability",
            kind: "scrollBy",
            top: correction,
            source: "layout-height-changed",
            scrollTop: element.scrollTop,
            scrollHeight: element.scrollHeight,
            clientHeight: element.clientHeight,
            bottomDistance: nativeTranscriptDistanceFromBottom(element),
            mode: modeRef.current,
          })) {
          guardRef.current = null;
          return;
        }
      }
      if (Date.now() >= active.deadline) {
        guardRef.current = null;
        return;
      }
      active.frame = requestAnimationFrame(tick);
    };
    guardRef.current = active;
    active.frame = requestAnimationFrame(tick);
  }, [cancel, generationRef, modeRef, scrollRef, writeCorrection]);

  useEffect(() => cancel, [cancel]);

  return useMemo(() => ({ arm, cancel, observe, isActive }), [arm, cancel, observe, isActive]);
}
