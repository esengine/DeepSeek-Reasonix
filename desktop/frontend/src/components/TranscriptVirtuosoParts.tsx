import { forwardRef, memo, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode, useContext, useEffect } from "react";
import { type Components, type ItemProps, type ListProps } from "react-virtuoso";
import { Loader2, RotateCcw } from "lucide-react";
import type { TranscriptEstimateSource, TranscriptGeometryEnvironment, TranscriptRowLayoutVariant } from "../lib/transcriptRowGeometry";
import { estimateTranscriptRowGeometry, transcriptRowLayoutVariant } from "../lib/transcriptRowGeometry";
import { getTranscriptStore } from "../lib/transcriptStore";
import { historyEntryIdForRow, transcriptRowMeasurementVersion, type AssistantItem, type TranscriptRow } from "../lib/transcriptRows";
import { TranscriptSelectionOverlay } from "./TranscriptSelectionOverlay";
import { AssistantMessage } from "./Message";
import { LiveTurnRegion } from "./LiveTurnRegion";
import { LiveStreamContext } from "./LiveStreamContext";
import { useT } from "../lib/i18n";

export const SHOW_SCROLL_DIAGNOSTICS = typeof __BUILD_CHANNEL__ === "undefined"
  || __BUILD_CHANNEL__ === "test"
  || __BUILD_CHANNEL__ === "preview"
  || __BUILD_CHANNEL__ === "canary"
  || Boolean(import.meta.env?.DEV);

export type TranscriptVirtuosoContext = {
  tabId?: string;
  scrollElement: HTMLDivElement | null;
  nativeScrollbarDragging: boolean;
  overlayRevision: string;
  geometryEnvironment: TranscriptGeometryEnvironment;
  rowGeometry: {
    heightEstimates: readonly number[];
    estimateSources: readonly TranscriptEstimateSource[];
    rowIndexByKey: ReadonlyMap<string, number>;
    contentRevision: number;
  };
  liveRegion: null | {
    rows: readonly TranscriptRow[];
    renderRow: (row: TranscriptRow) => ReactNode;
    showStatus: boolean;
    turnStartAt?: number;
    onPointerDownCapture: (event: ReactPointerEvent<HTMLElement>) => void;
  };
  olderHistory: null | {
    loading: boolean;
    error?: string;
    onRetry: () => void;
  };
};

export const LiveAssistantMessage = memo(function LiveAssistantMessage({
  item,
  defaultExpanded = false,
  expandWhileStreaming = false,
  creationMode = false,
  reasoningDisplay = "normal",
}: {
  item: AssistantItem;
  defaultExpanded?: boolean;
  expandWhileStreaming?: boolean;
  creationMode?: boolean;
  reasoningDisplay?: "normal" | "hide";
}) {
  const live = useContext(LiveStreamContext);
  const shown = {
    ...item,
    ...(live && live.id === item.id
      ? {
          text: live.text,
          reasoning: live.reasoning,
          streaming: true,
          reasoningComplete: live.reasoningComplete,
          reasoningDurationMs: live.reasoningStartedAt && live.reasoningCompletedAt && live.reasoningCompletedAt >= live.reasoningStartedAt
            ? live.reasoningCompletedAt - live.reasoningStartedAt
            : item.reasoningDurationMs,
        }
      : {}),
  };
  if (reasoningDisplay === "hide") {
    shown.reasoning = "";
    shown.reasoningComplete = true;
    shown.reasoningDurationMs = undefined;
  }
  return <AssistantMessage item={shown} defaultExpanded={defaultExpanded} expandWhileStreaming={expandWhileStreaming} creationMode={creationMode} />;
});

export const TranscriptVirtuosoItem = forwardRef<HTMLDivElement, ItemProps<TranscriptRow> & { context: TranscriptVirtuosoContext }>(
  function TranscriptVirtuosoItem({ item, context, children, style, ...props }, ref) {
    const entryId = historyEntryIdForRow(item);
    useEffect(() => {
      if (entryId) getTranscriptStore().requestEntryFullContent(context.tabId, entryId);
    }, [context.tabId, entryId]);
    const knownSize = Number.parseFloat(String(props["data-known-size"] ?? ""));
    const rowIndex = context.rowGeometry.rowIndexByKey.get(String(item.key)) ?? Number.NaN;
    const estimatedSize = Number.isInteger(rowIndex) && rowIndex >= 0 ? context.rowGeometry.heightEstimates[rowIndex] : undefined;
    const estimateSource = Number.isInteger(rowIndex) && rowIndex >= 0 ? context.rowGeometry.estimateSources[rowIndex] : undefined;
    const layoutVariant: TranscriptRowLayoutVariant = transcriptRowLayoutVariant(item);
    const staticEstimate = estimateTranscriptRowGeometry(item, context.geometryEnvironment);
    const rowEstimate = Number.isFinite(estimatedSize) && (estimatedSize ?? 0) > 0 ? estimatedSize : staticEstimate;
    const diagnosticAttributes = SHOW_SCROLL_DIAGNOSTICS
      ? {
          "data-logical-index": Number.isInteger(rowIndex) && rowIndex >= 0 ? rowIndex : undefined,
          "data-estimated-size": estimatedSize,
          "data-content-revision": context.rowGeometry.contentRevision,
          "data-estimate-source": estimateSource,
        }
      : {};
    const frozenStyle = context.nativeScrollbarDragging && Number.isFinite(knownSize) && knownSize > 0
      ? { ...style, boxSizing: "border-box" as const, height: knownSize, overflow: "hidden" as const }
      : style;
    const geometryStyle = Number.isFinite(rowEstimate) && (rowEstimate ?? 0) > 0
      ? { ...frozenStyle, "--transcript-row-estimate": `${rowEstimate}px` } as CSSProperties
      : frozenStyle;
    return (
      <div
        {...props}
        ref={ref}
        style={geometryStyle}
        data-row-key={String(item.key)}
        data-row-kind={item.kind}
        data-layout-version={transcriptRowMeasurementVersion(item)}
        data-transcript-layout-variant={layoutVariant}
        data-transcript-content-width={context.geometryEnvironment.contentWidth}
        data-transcript-estimate={rowEstimate}
        data-static-estimate={staticEstimate}
        {...diagnosticAttributes}
        className="transcript__row"
      >
        {children}
      </div>
    );
  },
);

const TranscriptVirtuosoList = forwardRef<HTMLDivElement, ListProps & { context: TranscriptVirtuosoContext }>(
  function TranscriptVirtuosoList({ context, children, ...props }, ref) {
    return <div {...props} ref={ref} className="transcript__virtual-sizer">
      <TranscriptSelectionOverlay tabId={context.tabId ?? ""} scrollElement={context.scrollElement} virtualRevision={context.overlayRevision} />
      {children}
    </div>;
  },
);

function TranscriptVirtuosoHeader({ context }: { context: TranscriptVirtuosoContext }) {
  const t = useT();
  const older = context.olderHistory;
  if (!older) return null;
  return <div className="transcript__header"><div className="transcript__older-status" role={older.error ? "alert" : "status"}>
    {older.loading ? <><Loader2 className="transcript__older-spinner" size={14} aria-hidden="true" /><span>{t("common.loading")}</span></> : <><span>{older.error}</span><button type="button" className="btn btn--small" onClick={older.onRetry}><RotateCcw size={14} /><span>{t("common.retry")}</span></button></>}
  </div></div>;
}

function TranscriptVirtuosoFooter({ context }: { context: TranscriptVirtuosoContext }) {
  const live = context.liveRegion;
  if (!live || (live.rows.length === 0 && !live.showStatus)) return null;
  return <LiveTurnRegion rows={live.rows} renderRow={live.renderRow} showStatus={live.showStatus} turnStartAt={live.turnStartAt} tabId={context.tabId} scrollElement={context.scrollElement} onPointerDownCapture={live.onPointerDownCapture} />;
}

export const TRANSCRIPT_VIRTUOSO_COMPONENTS: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  Item: TranscriptVirtuosoItem,
  List: TranscriptVirtuosoList,
  Footer: TranscriptVirtuosoFooter,
};

export const TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  ...TRANSCRIPT_VIRTUOSO_COMPONENTS,
  Header: TranscriptVirtuosoHeader,
};
