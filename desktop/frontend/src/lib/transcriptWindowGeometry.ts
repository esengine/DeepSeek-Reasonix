import { commitTranscriptWindowRange, type TranscriptWindowItem, type TranscriptWindowRange } from "./transcriptWindowRange";

type PrefixItem = TranscriptWindowItem & { key: string | number | bigint; size: number };
export const MAX_MOUNTED_COMPLETED_BLOCKS = 40;
export type TranscriptWindowGeometry<T extends PrefixItem> = {
  range: TranscriptWindowRange<T>;
  prefix: { items: readonly T[]; extent: number; margin: number };
  covered: boolean;
  mode: "full" | "windowed";
  measurementCommitted: boolean;
};

/** Own range, prefix, and extent together; third-party cache views are not snapshots. */
export function commitTranscriptWindowGeometry<T extends PrefixItem>(
  input: Omit<Parameters<typeof commitTranscriptWindowRange<T>>[0], "previous"> & {
    previous?: TranscriptWindowGeometry<T>;
    residentCount: number;
    forceFull: boolean;
    scrollHeight?: number;
    measurementCommit?: boolean;
  },
): TranscriptWindowGeometry<T> {
  // TanStack's single-lane view is a lazy Proxy backed by a mutable typed
  // array. map/every can skip its virtual indices; materialize before owning it.
  const items = Array.from(input.measurements, (item) => ({ ...item }));
  const valid = Number.isFinite(input.totalSize) && input.totalSize >= 0
    && (input.totalSize === 0 || items.length > 0)
    && items.every((item, index) => Number.isFinite(item.start) && Number.isFinite(item.end)
      && Number.isFinite(item.size) && item.size > 0 && Math.abs(item.end - item.start - item.size) <= 0.5
      && Math.abs(item.start - (items[index - 1]?.end ?? input.scrollMargin)) <= 0.5)
    && Math.abs((items[items.length - 1]?.end ?? input.scrollMargin) - input.scrollMargin - input.totalSize) <= 0.5;
  const previous = input.previous;
  let prefix = valid ? { items, extent: input.totalSize, margin: input.scrollMargin }
    : previous?.range.structureRevision === input.structureRevision ? previous.prefix : { items: [], extent: 0, margin: 0 };
  // An adapter-approved batch is a before-paint transaction. Retaining the
  // older prefix would defer safe offscreen growth until native travel brings
  // it into view. A stale candidate must instead reconstruct from this batch.
  const candidate = input.measurementCommit && valid ? input.candidate.flatMap(item => {
    const owned = items[item.index];
    return owned?.key === item.key ? [owned] : [];
  }) : input.candidate;
  const range = commitTranscriptWindowRange({ ...input, candidate, measurements: items,
    previous: input.measurementCommit && valid ? undefined : previous?.range });
  if (range.source === "retained" && previous) prefix = previous.prefix;
  const covered = valid && Number.isFinite(input.scrollHeight ?? 0) && range.covered
    && range.items.length + input.residentCount <= MAX_MOUNTED_COMPLETED_BLOCKS;
  return { range, prefix, covered, mode: input.forceFull || !covered ? "full" : "windowed",
    measurementCommitted: Boolean(input.measurementCommit && valid) };
}

/** Future publication is a geometry decision, independent of queued input units. */
export function findTranscriptMeasurementPublicationBoundary({
  paintedItems, domItems, scrollTop, clientHeight, anchorIndex,
}: {
  paintedItems: readonly { index: number; start: number }[];
  domItems: readonly { index: number; top: number }[];
  scrollTop: number;
  clientHeight: number;
  anchorIndex?: number;
}): number | undefined {
  if (!Number.isFinite(scrollTop) || !Number.isFinite(clientHeight) || clientHeight <= 0) return undefined;
  // One viewport of measured runway protects the current visible blocks. It
  // is not an estimate or limit for future compositor travel: the adapter
  // re-observes native geometry and commits each approved prefix before paint.
  const afterRunway = clientHeight * 2;
  const painted = paintedItems.find(item => item.start >= scrollTop + afterRunway - 0.5)?.index;
  const measured = domItems.find(item => item.top >= afterRunway - 0.5)?.index;
  return painted == null || measured == null ? undefined : Math.max(painted, measured, anchorIndex ?? 0);
}
