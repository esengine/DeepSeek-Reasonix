import { memo, useEffect, type CSSProperties, type ReactNode } from "react";
import { getTranscriptStore } from "../lib/transcriptStore";
import {
  historyEntryIdForRow,
  transcriptRowMeasurementVersion,
  estimateTranscriptRowSize,
  type TranscriptRow,
} from "../lib/transcriptRows";
import { transcriptRowLayoutVariant } from "../lib/transcriptRowGeometry";
import type { TimelineBlock } from "../lib/transcriptTimeline";

const TranscriptRowView = memo(function TranscriptRowView({
  row,
  tabId,
  children,
}: {
  row: TranscriptRow;
  tabId?: string;
  children: ReactNode;
}) {
  const entryId = historyEntryIdForRow(row);
  const estimate = estimateTranscriptRowSize(row);
  useEffect(() => {
    if (entryId) getTranscriptStore().requestEntryFullContent(tabId, entryId);
  }, [entryId, tabId]);
  return (
    <div
      className="transcript__row"
      data-row-key={String(row.key)}
      data-row-kind={row.kind}
      data-layout-version={transcriptRowMeasurementVersion(row)}
      data-transcript-layout-variant={transcriptRowLayoutVariant(row)}
      style={{ "--transcript-row-estimate": `${estimate}px` } as CSSProperties}
    >
      {children}
    </div>
  );
});

export const TranscriptBlockView = memo(function TranscriptBlockView({
  block,
  tabId,
  renderRow,
  placement,
}: {
  block: TimelineBlock;
  tabId?: string;
  renderRow: (row: TranscriptRow) => ReactNode;
  placement?: { index: number; top: number };
}) {
  return (
    <div
      className={`transcript__block${placement ? " transcript__window-item" : ""}`}
      data-index={placement?.index}
      style={placement ? { position: "absolute", top: placement.top, left: 0, width: "100%" } : undefined}
      data-transcript-block-key={block.key}
      data-transcript-block-phase={block.phase}
      data-transcript-content-revision={block.contentRevision}
      data-transcript-measurement-revision={block.measurementRevision}
    >
      {block.rows.map((row) => (
        <TranscriptRowView key={row.key} row={row} tabId={tabId}>
          {renderRow(row)}
        </TranscriptRowView>
      ))}
    </div>
  );
});
