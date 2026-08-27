import { useEffect } from "react";
import type { TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";

/**
 * Observe the rendered list box as a lossless producer for real row-size
 * changes that WebViews do not always report through itemSize. These signals
 * use the deferred row-measure path: aggregate totalListHeightChanged remains
 * diagnostics-only and a tail write cannot synchronously feed itself back.
 */
export function useTranscriptListGeometryObserver({
  scrollElement,
  enabled,
  surfaceKey,
  noteGeometryChange,
  observeReaderExtent,
}: {
  scrollElement: HTMLDivElement | null;
  enabled: boolean;
  surfaceKey: string;
  noteGeometryChange: (source: TranscriptGeometryChangeSource) => void;
  observeReaderExtent: () => boolean;
}) {
  useEffect(() => {
    if (!enabled || !scrollElement || typeof ResizeObserver === "undefined") return;
    const MutationObserverCtor = scrollElement.ownerDocument.defaultView?.MutationObserver;
    if (!MutationObserverCtor) return;
    let observedList: HTMLElement | null = null;
    let previousHeight = 0;
    let previousScrollHeight = scrollElement.scrollHeight;
    const observer = new ResizeObserver(() => {
      if (!observedList) return;
      const height = observedList.getBoundingClientRect().height;
      if (Math.abs(height - previousHeight) <= 0.5) return;
      // ResizeObserver sees the final laid-out range before paint. Recheck the
      // reader anchor here because a child-list mutation can arrive before
      // Virtuoso applies its translated range offset.
      observeReaderExtent();
      previousHeight = height;
      previousScrollHeight = scrollElement.scrollHeight;
      noteGeometryChange("row-measure");
    });
    const attachCurrentList = () => {
      if (observedList?.isConnected && scrollElement.contains(observedList)) return;
      observer.disconnect();
      observedList = scrollElement.querySelector<HTMLElement>(".transcript__virtual-sizer");
      if (!observedList) return;
      previousHeight = observedList.getBoundingClientRect().height;
      observer.observe(observedList);
    };
    // Virtuoso can mount or replace its sizer after the scroller ref and this
    // effect have committed. Track that lifecycle without turning every row
    // mutation into a geometry revision; once attached, the fast connected
    // check exits before querying the subtree.
    const mountObserver = new MutationObserverCtor(() => {
      attachCurrentList();
      // Virtuoso can replace its mounted range without changing native
      // scrollTop. MutationObserver runs after that DOM commit but before
      // paint, which is the last point where an active reader transaction can
      // reject the visible anchor displacement without flashing one frame.
      observeReaderExtent();
      // The translated range spacer can change the native extent while the
      // mounted item-list box keeps the same height. That is invisible to the
      // ResizeObserver above, but a pinned tail still needs one deferred
      // geometry revision to follow the new physical bottom. Compare the real
      // extent so ordinary row identity/style mutations remain diagnostics-
      // only and a scrollTop write cannot feed back into another revision.
      const scrollHeight = scrollElement.scrollHeight;
      if (Math.abs(scrollHeight - previousScrollHeight) > 0.5) {
        previousScrollHeight = scrollHeight;
        noteGeometryChange("row-measure");
      }
    });
    mountObserver.observe(scrollElement, {
      childList: true,
      subtree: true,
      attributes: true,
      // React Virtuoso can recycle an item shell by changing its logical row
      // identity without changing the sizer height or wrapper style. Observe
      // those identity commits too so the reader guard runs before paint.
      attributeFilter: ["style", "data-row-key", "data-item-index"],
    });
    attachCurrentList();
    return () => {
      mountObserver.disconnect();
      observer.disconnect();
    };
  }, [enabled, noteGeometryChange, observeReaderExtent, scrollElement, surfaceKey]);
}
