import { defaultRangeExtractor, type Range } from "@tanstack/react-virtual";

export interface TranscriptSelectionRowRange {
  anchorIndex: number;
  focusIndex: number;
}

export function createSelectionRangeExtractor(
  getSelection: () => TranscriptSelectionRowRange | null,
): (range: Range) => number[] {
  return (range) => {
    const normal = defaultRangeExtractor(range);
    const selection = getSelection();
    if (!selection) return normal;

    const first = Math.max(0, Math.min(selection.anchorIndex, selection.focusIndex));
    const last = Math.min(range.count - 1, Math.max(selection.anchorIndex, selection.focusIndex));
    if (last < first) return normal;

    const retained = new Set(normal);
    for (let index = first; index <= last; index += 1) retained.add(index);
    return Array.from(retained).sort((a, b) => a - b);
  };
}
