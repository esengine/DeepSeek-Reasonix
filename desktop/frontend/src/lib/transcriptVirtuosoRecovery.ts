import type { FlatIndexLocationWithAlign } from "react-virtuoso";

export type TranscriptRowRect = {
  rowKey: string;
  top: number;
  bottom: number;
};

export type TranscriptViewportRect = {
  top: number;
  bottom: number;
};

export type TranscriptLayoutAnchor =
  | { mode: "tail" }
  | { mode: "manual"; rowKey: string; offset: number };

function rowIntersectsViewport(row: TranscriptRowRect, viewport: TranscriptViewportRect): boolean {
  return row.bottom > viewport.top && row.top < viewport.bottom;
}

function distanceFromViewport(row: TranscriptRowRect, viewport: TranscriptViewportRect): number {
  if (row.bottom <= viewport.top) return viewport.top - row.bottom;
  if (row.top >= viewport.bottom) return row.top - viewport.bottom;
  return 0;
}

/** Select a stable logical anchor before a Virtuoso size-tree rebuild. */
export function chooseTranscriptLayoutAnchor(
  rows: readonly TranscriptRowRect[],
  viewport: TranscriptViewportRect,
  tailFollow: boolean,
): TranscriptLayoutAnchor | undefined {
  if (tailFollow) return { mode: "tail" };
  const visibleRows = rows
    .filter((row) => rowIntersectsViewport(row, viewport))
    .sort((left, right) => left.top - right.top);
  const visible = visibleRows.find((row) => row.top >= viewport.top) ?? visibleRows[0];
  if (visible) return { mode: "manual", rowKey: visible.rowKey, offset: visible.top - viewport.top };
  const nearest = [...rows].sort((left, right) => distanceFromViewport(left, viewport) - distanceFromViewport(right, viewport))[0];
  // A blank viewport already lost its physical anchor. Restore the nearest
  // logical row at the top instead of preserving an offscreen pixel offset.
  return nearest ? { mode: "manual", rowKey: nearest.rowKey, offset: 0 } : undefined;
}

export function transcriptAnchorInitialLocation(
  anchor: TranscriptLayoutAnchor | undefined,
  rowIndexByKey: ReadonlyMap<string, number>,
  firstItemIndex: number,
): FlatIndexLocationWithAlign | undefined {
  if (!anchor) return undefined;
  if (anchor.mode === "tail") return { index: "LAST", align: "end" };
  const index = rowIndexByKey.get(anchor.rowKey);
  if (index == null) return undefined;
  return { index: firstItemIndex + index, align: "start", offset: anchor.offset };
}

export function transcriptViewportIsBlank(
  rows: readonly TranscriptRowRect[],
  viewport: TranscriptViewportRect,
  scrollable: boolean,
): boolean {
  return scrollable && rows.length > 0 && !rows.some((row) => rowIntersectsViewport(row, viewport));
}

export function readTranscriptRowRects(element: HTMLElement): TranscriptRowRect[] {
  return Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
    .map((row) => {
      const rect = row.getBoundingClientRect();
      return { rowKey: row.dataset.rowKey ?? "", top: rect.top, bottom: rect.bottom };
    })
    .filter((row) => row.rowKey !== "" && Number.isFinite(row.top) && Number.isFinite(row.bottom));
}

export function captureTranscriptLayoutAnchor(
  element: HTMLElement,
  tailFollow: boolean,
): TranscriptLayoutAnchor | undefined {
  const rect = element.getBoundingClientRect();
  return chooseTranscriptLayoutAnchor(
    readTranscriptRowRects(element),
    { top: rect.top, bottom: rect.bottom },
    tailFollow,
  );
}

/** Steady-state variant: only anchors on a row that actually intersects the
 *  viewport. The nearest-row fallback in chooseTranscriptLayoutAnchor exists
 *  for blank-viewport restores; using it for viewport compensation would
 *  measure drift against an offscreen row and yank the reading position. */
export function captureVisibleTranscriptLayoutAnchor(
  element: HTMLElement,
): Extract<TranscriptLayoutAnchor, { mode: "manual" }> | undefined {
  const rect = element.getBoundingClientRect();
  const viewport = { top: rect.top, bottom: rect.bottom };
  const visible = readTranscriptRowRects(element)
    .filter((row) => rowIntersectsViewport(row, viewport))
    .sort((left, right) => left.top - right.top);
  const anchor = visible.find((row) => row.top >= viewport.top) ?? visible[0];
  return anchor ? { mode: "manual", rowKey: anchor.rowKey, offset: anchor.top - viewport.top } : undefined;
}

export function transcriptElementViewportIsBlank(element: HTMLElement): boolean {
  const rect = element.getBoundingClientRect();
  if (rect.bottom <= rect.top) return false;
  return transcriptViewportIsBlank(
    readTranscriptRowRects(element),
    { top: rect.top, bottom: rect.bottom },
    element.scrollHeight > element.clientHeight + 1,
  );
}
