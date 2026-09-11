export type TranscriptWindowItem = {
  index: number;
  start: number;
  end: number;
};

export type TranscriptWindowRangeSource = "candidate" | "retained" | "reconstructed" | "unavailable";
export type TranscriptWindowDirection = "forward" | "backward" | null;

export function extractTranscriptWindowIndexes(
  range: { startIndex: number; endIndex: number; count: number },
  retainedIndexes: ReadonlySet<number>,
  maxItems: number,
  direction: TranscriptWindowDirection,
): number[] {
  const indexes = new Set<number>();
  for (let index = range.startIndex; index <= range.endIndex; index += 1) indexes.add(index);
  retainedIndexes.forEach((index) => {
    if (index >= 0 && index < range.count) indexes.add(index);
  });
  const limit = Math.max(maxItems, indexes.size);
  const addBefore = (count = Number.POSITIVE_INFINITY) => {
    let added = 0;
    for (let index = range.startIndex - 1; index >= 0 && indexes.size < limit && added < count; index -= 1) {
      const size = indexes.size;
      indexes.add(index);
      if (indexes.size > size) added += 1;
    }
  };
  const addAfter = (count = Number.POSITIVE_INFINITY) => {
    let added = 0;
    for (let index = range.endIndex + 1; index < range.count && indexes.size < limit && added < count; index += 1) {
      const size = indexes.size;
      indexes.add(index);
      if (indexes.size > size) added += 1;
    }
  };
  const reverseRunway = 4;
  if (direction === "forward") {
    addBefore(reverseRunway);
    addAfter();
    addBefore();
  } else if (direction === "backward") {
    addAfter(reverseRunway);
    addBefore();
    addAfter();
  } else {
    for (let offset = 1; indexes.size < limit && (range.startIndex - offset >= 0 || range.endIndex + offset < range.count); offset += 1) {
      if (range.startIndex - offset >= 0) indexes.add(range.startIndex - offset);
      if (indexes.size < limit && range.endIndex + offset < range.count) indexes.add(range.endIndex + offset);
    }
  }
  return Array.from(indexes).sort((left, right) => left - right);
}

export type TranscriptWindowRange<T extends TranscriptWindowItem> = {
  structureRevision: string;
  scrollTop: number;
  scrollMargin: number;
  totalSize: number;
  items: readonly T[];
  source: TranscriptWindowRangeSource;
  covered: boolean;
};

function coversColdViewport<T extends TranscriptWindowItem>(
  items: readonly T[],
  scrollTop: number,
  clientHeight: number,
  coldStart: number,
  coldEnd: number,
): boolean {
  if (![scrollTop, clientHeight, coldStart, coldEnd].every(Number.isFinite) || coldEnd < coldStart) return false;
  const start = Math.max(scrollTop, coldStart);
  const end = Math.min(scrollTop + clientHeight, coldEnd);
  if (end <= start) return true;
  let cursor = start;
  for (const item of [...items].sort((left, right) => left.start - right.start)) {
    if (item.end <= cursor) continue;
    if (item.start > cursor + 0.5) return false;
    cursor = Math.max(cursor, item.end);
    if (cursor >= end - 0.5) return true;
  }
  return false;
}

function reconstructRange<T extends TranscriptWindowItem>(
  measurements: readonly T[],
  retainedIndexes: ReadonlySet<number>,
  scrollTop: number,
  clientHeight: number,
  coldStart: number,
  coldEnd: number,
  maxItems: number,
  direction: TranscriptWindowDirection,
): readonly T[] {
  const start = Math.max(scrollTop, coldStart);
  const end = Math.min(scrollTop + clientHeight, coldEnd);
  if (end <= start) return measurements.filter((item) => retainedIndexes.has(item.index));
  const first = measurements.findIndex((item) => item.end > start);
  if (first < 0) return [];
  let last = first;
  while (last + 1 < measurements.length && measurements[last + 1].start < end) last += 1;
  return extractTranscriptWindowIndexes({ startIndex: first, endIndex: last, count: measurements.length }, retainedIndexes, maxItems, direction)
    .map((index) => measurements[index])
    .filter((item): item is T => Boolean(item));
}

export function commitTranscriptWindowRange<T extends TranscriptWindowItem>({
  candidate,
  measurements,
  retainedIndexes,
  previous,
  structureRevision,
  scrollTop,
  clientHeight,
  scrollMargin,
  totalSize,
  maxItems,
  direction,
  gestureActive,
}: {
  candidate: readonly T[];
  measurements: readonly T[];
  retainedIndexes: ReadonlySet<number>;
  previous?: TranscriptWindowRange<T>;
  structureRevision: string;
  scrollTop: number;
  clientHeight: number;
  scrollMargin: number;
  totalSize: number;
  maxItems: number;
  direction: TranscriptWindowDirection;
  gestureActive: boolean;
}): TranscriptWindowRange<T> {
  const coldStart = scrollMargin;
  const coldEnd = scrollMargin + totalSize;
  const next: TranscriptWindowRange<T> = { structureRevision, scrollTop, scrollMargin, totalSize, items: candidate, source: "candidate", covered: false };
  // Overscan is optional. Re-budget it against today's resident/protected set
  // before accepting either a new candidate or an immutable prior snapshot.
  const fit = (items: readonly T[], start: number, end: number): readonly T[] => {
    if (items.length <= maxItems) return items;
    const required = items.filter((item) => retainedIndexes.has(item.index)
      || (item.end > Math.max(scrollTop, start) && item.start < Math.min(scrollTop + clientHeight, end)));
    const keys = new Set(required.map((item) => item.index));
    const optional = items.filter((item) => !keys.has(item.index))
      .sort((a, b) => Math.abs(a.start - scrollTop) - Math.abs(b.start - scrollTop));
    return [...required, ...optional.slice(0, Math.max(0, maxItems - required.length))].sort((a, b) => a.index - b.index);
  };
  const usable = (items: readonly T[], start: number, end: number) => items.length <= maxItems
    && [...retainedIndexes].every((index) => items.some((item) => item.index === index))
    && coversColdViewport(items, scrollTop, clientHeight, start, end);
  const fittedCandidate = fit(candidate, coldStart, coldEnd);
  const fittedPrevious = previous && fit(previous.items, previous.scrollMargin, previous.scrollMargin + previous.totalSize);
  const sameStructure = previous?.structureRevision === structureRevision;
  const sameMargin = previous != null && Math.abs(previous.scrollMargin - scrollMargin) <= 0.5;
  const previousCovers = Boolean(sameStructure && sameMargin && fittedPrevious && usable(
    fittedPrevious,
    previous.scrollMargin,
    previous.scrollMargin + previous.totalSize,
  ));
  const candidateCovers = usable(fittedCandidate, coldStart, coldEnd);

  // A measurement-only notification must not move the painted reader range
  // while native input still owns the unchanged viewport.
  if (previous && previousCovers && gestureActive && Math.abs(previous.scrollTop - scrollTop) <= 0.5) {
    return { ...previous, items: fittedPrevious!, source: "retained", covered: true };
  }
  if (candidateCovers) return { ...next, items: fittedCandidate, covered: true };

  // Native WebViews may deliver a stale range notification after a newer
  // scroll position was already painted. Retain the last covering range until
  // TanStack produces a candidate that covers the authoritative native view.
  if (previous && previousCovers) {
    return { ...previous, items: fittedPrevious!, scrollTop, source: "retained", covered: true };
  }

  // A large native jump can invalidate both the candidate and the previously
  // painted range. Rebuild synchronously from TanStack's prefix-size ledger so
  // the adapter never commits an uncovered viewport while waiting for its next
  // asynchronous range notification.
  const reconstructed = reconstructRange(measurements, retainedIndexes, scrollTop, clientHeight, coldStart, coldEnd, maxItems, direction);
  if (usable(reconstructed, coldStart, coldEnd)) {
    return { structureRevision, scrollTop, scrollMargin, totalSize, items: reconstructed, source: "reconstructed", covered: true };
  }
  // Never paint a range that leaves the authoritative native viewport
  // uncovered. The adapter renders the same projection through its full-DOM
  // safety path until a covering immutable range is available.
  return { ...next, items: [], source: "unavailable", covered: false };
}
