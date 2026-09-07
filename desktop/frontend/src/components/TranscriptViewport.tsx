import { Loader2, RotateCcw } from "lucide-react";
import { forwardRef, lazy, Suspense, useImperativeHandle, useLayoutEffect, useState, type ReactNode } from "react";
import { estimateTranscriptRowSize, type TranscriptRow } from "../lib/transcriptRows";
import type { TranscriptKernel } from "../lib/transcriptKernel";
import type { TimelineBlock, TimelineProjection, TranscriptRenderMode } from "../lib/transcriptTimeline";
import { useT } from "../lib/i18n";
import { TranscriptSelectionOverlay } from "./TranscriptSelectionOverlay";
import { TranscriptProjectionView } from "./TranscriptProjectionView";
import { ProcessBrainIcon } from "./ProcessCard";
import { useTick, workStatusLabel } from "../lib/workStatus";

const TranscriptWindow = lazy(() => import("./TranscriptWindow"));
function estimateBlock(block: TimelineBlock): number {
  return Math.max(64, block.rows.reduce((height, row) => height + estimateTranscriptRowSize(row), 0));
}
export type TranscriptViewportHandle = { mountBlock: (blockKey: string) => void };

function ActiveTurnStatus({ turnStartAt }: { turnStartAt?: number }) {
  const t = useT();
  const now = useTick(true);
  const durationMs = turnStartAt ? Math.max(0, now - turnStartAt) : 0;
  return <div className="transcript__live-status" data-kind="reasoning"><ProcessBrainIcon size={12} /><span>{workStatusLabel(durationMs, true, t)}</span></div>;
}

export const TranscriptViewport = forwardRef<TranscriptViewportHandle, {
  projection: TimelineProjection;
  mode: TranscriptRenderMode;
  tabId?: string;
  scrollElement: HTMLDivElement | null;
  renderRow: (row: TranscriptRow) => ReactNode;
  loadingOlderHistory: boolean;
  olderHistoryError?: string;
  onRetryOlderHistory: () => void;
  onGeometryWillChange: () => unknown;
  onGeometryChange: (covered?: boolean) => void;
  kernel: TranscriptKernel;
  protectedBlockKeys?: ReadonlySet<string>;
  running: boolean;
  turnStartAt?: number;
}>(function TranscriptViewport({ projection, mode, tabId, scrollElement, renderRow,
  loadingOlderHistory, olderHistoryError, onRetryOlderHistory, onGeometryWillChange,
  onGeometryChange, kernel, protectedBlockKeys = new Set(),
  running, turnStartAt,
}, ref) {
  const t = useT();
  const [pinnedJumpBlockKey, setPinnedJumpBlockKey] = useState<string>();
  // Retain the adapter host through safety mode to preserve selected/focused DOM.
  const [windowLoaded, setWindowLoaded] = useState(mode === "windowed");
  useLayoutEffect(() => { if (mode === "windowed") setWindowLoaded(true); }, [mode]);
  useImperativeHandle(ref, () => ({ mountBlock: setPinnedJumpBlockKey }), []);
  const prefix = projection.hasOlderHistory && (loadingOlderHistory || olderHistoryError) && (
    <div className="transcript__header"><div className="transcript__older-status" role={olderHistoryError ? "alert" : "status"}>
      {loadingOlderHistory
        ? <><Loader2 className="transcript__older-spinner" size={14} aria-hidden="true" /><span>{t("common.loading")}</span></>
        : <><span>{t("transcript.loadEarlierFailed")}</span><button type="button" className="btn btn--small" onClick={onRetryOlderHistory}><RotateCcw size={14} /><span>{t("common.retry")}</span></button></>}
    </div></div>
  );
  const activeStatus = running && projection.activeBlock && projection.activeBlock.rows.length <= 1
    ? <ActiveTurnStatus turnStartAt={turnStartAt} /> : undefined;
  const shared = { tabId, scrollElement, renderRow, onGeometryWillChange, onGeometryChange, kernel, prefix, activeStatus };
  const renderSelectionOverlay = (revision: string) => <TranscriptSelectionOverlay tabId={tabId ?? ""} scrollElement={scrollElement} virtualRevision={revision} />;
  const full = <TranscriptProjectionView {...shared} mode="full" completedCount={projection.completedBlocks.length}
    revision={projection.completedBlocks.map((block) => block.measurementRevision).join("|") + projection.activeBlock?.measurementRevision}
    blocks={[...projection.completedBlocks, ...(projection.activeBlock ? [projection.activeBlock] : [])]}
    overlay={renderSelectionOverlay("full")} />;
  if (mode === "full" && !windowLoaded) return full;
  return <Suspense fallback={full}>
    <TranscriptWindow {...shared} projection={projection} forceFull={mode === "full"}
      protectedBlockKeys={protectedBlockKeys}
      pinnedJumpBlockKey={pinnedJumpBlockKey} onPinnedJumpVisible={() => setPinnedJumpBlockKey(undefined)}
      estimateBlock={estimateBlock} renderProjection={(layout) => <TranscriptProjectionView {...shared} {...layout} overlay={renderSelectionOverlay(layout.revision)} />} />
  </Suspense>;
});
