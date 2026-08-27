import {
  isTranscriptContentShrink,
  type TranscriptScrollEvent,
  type TranscriptScrollMode,
} from "./transcriptScrollArbiter";
import {
  nativeTranscriptDistanceFromBottom,
} from "./transcriptScrollGeometry";
import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";

type MutableRef<T> = { current: T };

export type TranscriptGeometryChangeSource =
  | "live-footer"
  | "row-measure"
  | "data"
  | "viewport"
  | "composer"
  | "typography"
  | "user-resize";

export type TranscriptGeometryRevision = readonly [
  cancel: () => void,
  note: (source: TranscriptGeometryChangeSource) => void,
  observeListHeight: (height: number) => void,
  reset: () => void,
];

export function createTranscriptGeometryRevision({
  scrollRef,
  modeRef,
  generationRef,
  revisionRef,
  dispatch,
  noteLayoutTransient,
  observeReaderExtent,
  scheduleAnchorCompensation,
}: {
  scrollRef: MutableRef<HTMLDivElement | null>;
  modeRef: MutableRef<TranscriptScrollMode>;
  generationRef: MutableRef<number>;
  revisionRef: MutableRef<number>;
  dispatch: (event: TranscriptScrollEvent) => void;
  noteLayoutTransient: () => void;
  observeReaderExtent: () => boolean;
  scheduleAnchorCompensation: () => void;
}): TranscriptGeometryRevision {
  const sources = new Set<TranscriptGeometryChangeSource>();
  let frame: number | null = null;
  let contentExtent: number | null = null;

  const cancel = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
  };

  const note = (source: TranscriptGeometryChangeSource) => {
    sources.add(source);
    noteLayoutTransient();
    observeReaderExtent();
    const firstSignalThisFrame = frame === null;
    const generation = generationRef.current;
    const scrollElement = scrollRef.current;
    if (firstSignalThisFrame && scrollElement) {
      const previous = contentExtent;
      contentExtent = scrollElement.scrollHeight;
      revisionRef.current += 1;
      if (previous != null && isTranscriptContentShrink(scrollElement.scrollHeight - previous)) {
        dispatch({ type: "CONTENT_SHRANK" });
      }
    }

    // ResizeObserver runs after layout but before paint. Publish the coalesced
    // revision in that frame. Footer/data changes may pin immediately; real
    // row measurements wait for stable geometry so a tail write cannot change
    // the mount window and feed its aggregate height back into another write.
    if (scrollElement && revisionRef.current > 0) {
      dispatch({
        type: "GEOMETRY_CHANGED",
        revision: revisionRef.current,
        deferUntilStable: sources.size === 1 && sources.has("row-measure"),
      });
    }
    if (!firstSignalThisFrame) return;
    frame = requestAnimationFrame(() => {
      frame = null;
      if (generationRef.current !== generation || scrollRef.current !== scrollElement) return;
      const element = scrollRef.current;
      if (element) {
        contentExtent = element.scrollHeight;
        const revision = revisionRef.current;
        const geometrySources = Array.from(sources).sort().join(":");
        sources.clear();
        recordTranscriptScrollDiagnostic("geometry-revision", {
          source: "geometry-changed",
          geometrySources: geometrySources || "unknown",
          geometryRevision: revision,
          scrollTop: element.scrollTop,
          scrollHeight: element.scrollHeight,
          clientHeight: element.clientHeight,
          bottomDistance: nativeTranscriptDistanceFromBottom(element),
          mode: modeRef.current,
          footerHeight: element.querySelector<HTMLElement>("[data-live-region='true']")?.getBoundingClientRect().height ?? 0,
          mountedRows: element.querySelectorAll(".transcript__row").length,
          totalRows: Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10),
        });
      }
      scheduleAnchorCompensation();
    });
  };

  const observeListHeight = (height: number) => {
    recordTranscriptScrollDiagnostic("list-height", {
      listHeight: height,
      geometryRevision: revisionRef.current,
      mode: modeRef.current,
    });
  };

  const reset = () => {
    cancel();
    contentExtent = null;
    sources.clear();
    revisionRef.current = 0;
  };

  return [cancel, note, observeListHeight, reset];
}
