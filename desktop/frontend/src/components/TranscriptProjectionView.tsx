import { useEffect, useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { observeTranscriptGeometry } from "../lib/transcriptGeometryObserver";
import type { TranscriptKernel } from "../lib/transcriptKernel";
import type { TimelineBlock } from "../lib/transcriptTimeline";
import type { TranscriptRow } from "../lib/transcriptRows";
import { TranscriptBlockView } from "./TranscriptBlockView";

export type ProjectionViewProps = {
  blocks: readonly TimelineBlock[];
  placements?: ReadonlyMap<string, { index: number; top: number }>;
  extent?: number;
  spacerRef?: RefObject<HTMLDivElement | null>;
  tailRef?: RefObject<HTMLDivElement | null>;
  mode: "full" | "windowed";
  safety?: boolean;
  completedCount: number;
  revision: string;
  prefix?: ReactNode;
  overlay?: ReactNode;
  activeStatus?: ReactNode;
  tabId?: string;
  renderRow: (row: TranscriptRow) => ReactNode;
  scrollElement: HTMLDivElement | null;
  kernel: Pick<TranscriptKernel, "generation" | "afterCurrentGenerationPaint">;
  onGeometryWillChange: () => unknown;
  onGeometryChange: () => void;
};

/** A block keeps its React and native host when it becomes cold or enters safety. */
export function TranscriptProjectionView({ blocks, placements, extent = 0, spacerRef, tailRef,
  mode, safety, completedCount, revision, prefix, overlay, activeStatus, tabId,
  renderRow, kernel, onGeometryWillChange, onGeometryChange,
}: ProjectionViewProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (mode === "full" && !safety) onGeometryWillChange();
    return kernel.afterCurrentGenerationPaint(onGeometryChange);
  }, [kernel, mode, onGeometryChange, onGeometryWillChange, revision, safety]);
  useEffect(() => {
    const element = rootRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    return observeTranscriptGeometry(kernel, element, () => { if (mode === "full" && !safety) onGeometryWillChange(); }, onGeometryChange);
  }, [kernel, mode, onGeometryChange, onGeometryWillChange, safety]);
  return <div ref={rootRef} className="transcript__projection" data-transcript-render-mode={mode}
    data-transcript-safe-fallback={safety || undefined} data-transcript-completed-blocks={completedCount}
    data-transcript-mounted-blocks={blocks.length}>
    {prefix}{overlay}
    <div ref={tailRef} className="transcript__resident-tail" data-transcript-resident-tail="true">
      <div ref={spacerRef} className="transcript__window" style={{ height: extent }} />
      {blocks.map((block) => <TranscriptBlockView key={block.key} block={block}
        placement={placements?.get(block.key)} tabId={tabId} renderRow={renderRow} />)}
      {activeStatus}
    </div>
  </div>;
}
