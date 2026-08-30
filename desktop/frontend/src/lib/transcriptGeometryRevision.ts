import type { RefObject } from "react";
import { isTranscriptContentShrink, type TranscriptScrollEvent } from "./transcriptScrollArbiter";
import { CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS } from "./transcriptScrollDiagnosticProbe";
import { MIN_REVERSE_JUMP_PX } from "./transcriptReaderExtentStability";
import { pinTranscriptTailAfterViewportShrink, type TranscriptFollowGeometry } from "./transcriptScrollGeometry";
import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";
import type { TranscriptTailSettle } from "./transcriptTailSettle";

export type TranscriptGeometryChangeSource =
  | "footer-resize"
  | "row-measure"
  | "data-change"
  | "viewport-resize"
  | "fold-change"
  | "typography-change"
  | "items-rendered";

export type TranscriptGeometryRevisionController = {
  cancel: () => void;
  note: (source?: TranscriptGeometryChangeSource) => void;
  reset: () => void;
  setViewport: (height: number | null) => void;
};

/** Coalesces all external layout observations into one revision and one tail lane. */
export function createTranscriptGeometryRevisionController({
  scrollRef,
  pinnedRef,
  generationRef,
  geometryRevisionRef,
  tailSettle,
  observeReader,
  dispatch,
  scheduleAnchor,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  pinnedRef: RefObject<boolean>;
  generationRef: RefObject<number>;
  geometryRevisionRef: RefObject<number>;
  tailSettle: TranscriptTailSettle;
  observeReader: () => boolean;
  dispatch: (event: TranscriptScrollEvent) => unknown;
  scheduleAnchor: () => void;
}): TranscriptGeometryRevisionController {
  const sources = new Set<TranscriptGeometryChangeSource>();
  let transient: { baseline: number; candidate: number; stableFrames: number } | null = null;
  let frame: number | null = null;
  let geometry: TranscriptFollowGeometry = { contentExtent: null, viewportExtent: null };

  const record = (element: HTMLDivElement, isTransient: boolean, result?: string) => {
    recordTranscriptScrollDiagnostic("geometry-revision", {
      revision: geometryRevisionRef.current,
      sources: [...sources],
      scrollHeight: element.scrollHeight,
      footerHeight: element.querySelector<HTMLElement>('[data-live-region="true"]')?.getBoundingClientRect().height ?? 0,
      viewport: element.clientHeight,
      mounted: element.querySelectorAll(".transcript__row[data-index]").length,
      total: +(element.dataset.transcriptRowCount ?? 0),
      transient: isTransient,
      result,
    });
  };

  const cancel = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
    sources.clear();
    transient = null;
  };

  const note = (source: TranscriptGeometryChangeSource = "row-measure") => {
    sources.add(source);
    tailSettle.noteLayoutTransient(source);
    observeReader();
    const pinnedTop = scrollRef.current
      && pinTranscriptTailAfterViewportShrink(scrollRef.current, geometry, pinnedRef.current);
    if (frame === null) geometryRevisionRef.current += 1;
    if (pinnedTop !== null) {
      tailSettle.scrollToTail("auto", CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS
        ? { source: "viewport-resized", phase: "initial" }
        : undefined);
    }
    if (frame !== null) return;
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    const sample = () => {
      frame = null;
      if (generationRef.current !== generation || scrollRef.current !== scrollElement) return;
      const element = scrollRef.current;
      if (element) {
        // Virtuoso may commit a new measured range after the source callback
        // that called note(). Re-observe inside the coalesced pre-paint frame
        // so an active reader can mask that first displaced range rather than
        // correcting it one painted frame later.
        observeReader();
        const scrollHeight = element.scrollHeight;
        const previous = geometry.contentExtent;
        const transientThreshold = Math.max(MIN_REVERSE_JUMP_PX, element.clientHeight * 0.5);
        if (transient) {
          if (scrollHeight >= transient.baseline - 1) {
            transient = null;
          } else if (Math.abs(scrollHeight - transient.candidate) <= 1) {
            transient.stableFrames += 1;
            transient.candidate = scrollHeight;
            if (transient.stableFrames < 2) {
              record(element, true);
              frame = requestAnimationFrame(sample);
              return;
            }
            transient = null;
            geometry.contentExtent = scrollHeight;
            record(element, false, "stable");
            dispatch({ type: "CONTENT_SHRANK" });
            scheduleAnchor();
            sources.clear();
            return;
          } else {
            transient.candidate = scrollHeight;
            transient.stableFrames = 0;
            frame = requestAnimationFrame(sample);
            return;
          }
        }
        if (previous != null && previous - scrollHeight >= transientThreshold) {
          transient = { baseline: previous, candidate: scrollHeight, stableFrames: 0 };
          record(element, true);
          frame = requestAnimationFrame(sample);
          return;
        }
        geometry.contentExtent = scrollHeight;
        record(element, false);
        sources.clear();
        if (previous != null && isTranscriptContentShrink(scrollHeight - previous)) {
          dispatch({ type: "CONTENT_SHRANK" });
          scheduleAnchor();
          return;
        }
      }
      dispatch({ type: "LAYOUT_HEIGHT_CHANGED" });
      scheduleAnchor();
    };
    frame = requestAnimationFrame(sample);
  };

  return {
    cancel,
    note,
    reset: () => {
      cancel();
      geometry = { contentExtent: null, viewportExtent: null };
    },
    setViewport: (height) => { geometry.viewportExtent = height; },
  };
}
