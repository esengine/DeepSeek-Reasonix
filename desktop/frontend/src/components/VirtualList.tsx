import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ReactNode, RefObject } from "react";

type EstimateSize<T> = number | ((item: T, index: number) => number);

export function VirtualList<T>({
  items,
  scrollRef,
  estimateSize,
  overscan = 6,
  scrollToIndex,
  getKey,
  render,
}: {
  items: T[];
  scrollRef: RefObject<HTMLElement | null>;
  estimateSize: EstimateSize<T>;
  overscan?: number;
  scrollToIndex?: number;
  getKey: (item: T, index: number) => string | number;
  render: (item: T, index: number) => ReactNode;
}) {
  const listRef = useRef<HTMLDivElement>(null);
  const [scrollMargin, setScrollMargin] = useState(0);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    getItemKey: (index) => {
      const item = items[index];
      return item === undefined ? index : getKey(item, index);
    },
    estimateSize: (index) => {
      const item = items[index];
      if (typeof estimateSize === "function") {
        return item === undefined ? 0 : estimateSize(item, index);
      }
      return estimateSize;
    },
    overscan,
    scrollMargin,
  });
  const virtualItems = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    const listEl = listRef.current;
    const scrollEl = scrollRef.current;
    if (!listEl || !scrollEl || listEl === scrollEl) {
      setScrollMargin((current) => (current === 0 ? current : 0));
      return;
    }
    const update = () => {
      const listRect = listEl.getBoundingClientRect();
      const scrollRect = scrollEl.getBoundingClientRect();
      const next = Math.max(0, Math.round(listRect.top - scrollRect.top + scrollEl.scrollTop));
      setScrollMargin((current) => (current === next ? current : next));
    };
    update();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    observer?.observe(listEl);
    observer?.observe(scrollEl);
    window.addEventListener("resize", update);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [items.length, scrollRef]);

  useEffect(() => {
    if (scrollToIndex === undefined || scrollToIndex < 0 || scrollToIndex >= items.length) return;
    virtualizer.scrollToIndex(scrollToIndex, { align: "auto" });
  }, [items.length, scrollToIndex, virtualizer]);

  return (
    <div className="virtual-list" ref={listRef} style={{ height: virtualizer.getTotalSize() }}>
      {virtualItems.map((virtualRow) => {
        const item = items[virtualRow.index];
        if (item === undefined) return null;
        return (
          <div
            key={getKey(item, virtualRow.index)}
            className="virtual-list__row"
            data-index={virtualRow.index}
            ref={virtualizer.measureElement}
            style={{ transform: `translateY(${virtualRow.start - scrollMargin}px)` }}
          >
            {render(item, virtualRow.index)}
          </div>
        );
      })}
    </div>
  );
}
