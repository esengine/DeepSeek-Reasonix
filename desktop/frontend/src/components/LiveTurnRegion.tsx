// LiveTurnRegion — the active ("streaming") turn rendered as the virtual
// list's in-flow Footer. It lives inside the transcript scroller but outside
// Virtuoso's measured size tree: unbounded, per-frame-growing content flows
// right after the last history row in plain document flow, so streaming never
// churns the list's measurements, anchors, or recovery machinery
// (#8657/#8688). Virtuoso tracks the footer height itself and includes it in
// totalListHeightChanged, which drives the scroll coordinator's tail-follow.

import { memo, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import type { TranscriptRow } from "../lib/transcriptRows";
import { useT } from "../lib/i18n";
import { useTick, workStatusLabel } from "../lib/workStatus";
import { ProcessBrainIcon } from "./ProcessCard";
import { TranscriptSelectionOverlay } from "./TranscriptSelectionOverlay";

function LiveTurnStatus({ turnStartAt }: { turnStartAt?: number }) {
  const t = useT();
  const now = useTick(true);
  const durationMs = turnStartAt ? Math.max(0, now - turnStartAt) : 0;
  return (
    <div className="transcript__live-status" data-kind="reasoning">
      <ProcessBrainIcon size={12} />
      <span>{workStatusLabel(durationMs, true, t)}</span>
    </div>
  );
}

export const LiveTurnRegion = memo(function LiveTurnRegion({
  rows,
  renderRow,
  showStatus,
  turnStartAt,
  tabId,
  scrollElement,
  onPointerDownCapture,
}: {
  rows: readonly TranscriptRow[];
  renderRow: (row: TranscriptRow) => ReactNode;
  /** Show the working status line when the turn has no rows yet. */
  showStatus: boolean;
  turnStartAt?: number;
  tabId?: string;
  scrollElement: HTMLElement | null;
  onPointerDownCapture?: (event: ReactPointerEvent<HTMLElement>) => void;
}) {
  const overlayRevision = rows.map((row) => String(row.key)).join("|");
  return (
    <div
      className="transcript__live-region"
      data-live-region="true"
      onPointerDownCapture={onPointerDownCapture}
    >
      <div className="transcript__live-content">
        <TranscriptSelectionOverlay
          tabId={tabId ?? ""}
          scrollElement={scrollElement}
          virtualRevision={overlayRevision}
        />
        {rows.map((row) => (
          <div key={String(row.key)} className="transcript__row" data-row-key={String(row.key)}>
            {renderRow(row)}
          </div>
        ))}
        {rows.length === 0 && showStatus ? <LiveTurnStatus turnStartAt={turnStartAt} /> : null}
      </div>
    </div>
  );
});
