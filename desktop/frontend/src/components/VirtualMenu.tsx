import { useEffect, useRef, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

// VirtualMenu is the shared scroll container for the composer's "/" and "@"
// dropdowns. Rows are virtualized so a directory with thousands of entries (or a
// long command list) only ever mounts the visible rows plus a small overscan —
// no truncation, no jank. The caller owns the item data, the active index, and
// per-row markup; this owns layout and keeping the active row in view.
export function VirtualMenu<T>({
  items,
  activeIndex,
  itemKey,
  renderItem,
  estimateSize,
}: {
  items: T[];
  activeIndex: number;
  itemKey: (item: T, index: number) => string;
  renderItem: (item: T, index: number) => ReactNode;
  estimateSize?: (item: T, index: number) => number;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: (index) => estimateSize?.(items[index], index) ?? 34,
    overscan: 10,
    // Measurement callbacks can arrive during React's commit phase. Let the
    // virtualizer update stable row positions directly instead of dispatching a
    // reducer update for every ResizeObserver measurement (React #185).
    directDomUpdates: true,
  });

  useEffect(() => {
    if (activeIndex >= 0 && activeIndex < items.length) {
      virtualizer.scrollToIndex(activeIndex, { align: "auto" });
    }
  }, [activeIndex, items.length, virtualizer]);

  return (
    <div ref={scrollRef} className="slashmenu" role="listbox">
      <div ref={virtualizer.containerRef} className="slashmenu__sizer">
        {virtualizer.getVirtualItems().map((row) => (
          <div
            key={itemKey(items[row.index], row.index)}
            data-index={row.index}
            ref={virtualizer.measureElement}
            className="slashmenu__row"
          >
            {renderItem(items[row.index], row.index)}
          </div>
        ))}
      </div>
    </div>
  );
}
