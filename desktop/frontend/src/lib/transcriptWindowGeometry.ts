import { commitTranscriptWindowRange, type TranscriptWindowItem, type TranscriptWindowRange } from "./transcriptWindowRange";

type PrefixItem = TranscriptWindowItem & { key: string | number | bigint; size: number };
export const MAX_MOUNTED_COMPLETED_BLOCKS = 40;
export type TranscriptWindowGeometry<T extends PrefixItem> = {
  range: TranscriptWindowRange<T>;
  prefix: { items: readonly T[]; extent: number; margin: number };
  covered: boolean;
  mode: "full" | "windowed";
};

/** Own range, prefix, and extent together; third-party cache views are not snapshots. */
export function commitTranscriptWindowGeometry<T extends PrefixItem>(
  input: Omit<Parameters<typeof commitTranscriptWindowRange<T>>[0], "previous"> & {
    previous?: TranscriptWindowGeometry<T>;
    residentCount: number;
    forceFull: boolean;
    scrollHeight?: number;
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
  const range = commitTranscriptWindowRange({ ...input, measurements: items, previous: previous?.range });
  if (range.source === "retained" && previous) prefix = previous.prefix;
  const covered = valid && Number.isFinite(input.scrollHeight ?? 0) && range.covered
    && range.items.length + input.residentCount <= MAX_MOUNTED_COMPLETED_BLOCKS;
  return { range, prefix, covered, mode: input.forceFull || !covered ? "full" : "windowed" };
}
