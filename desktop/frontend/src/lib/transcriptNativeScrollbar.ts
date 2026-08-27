import type { SizeFunction } from "react-virtuoso";

type ScrollbarPointer = Pick<PointerEvent, "button" | "clientX">;

/**
 * Native scrollbar pointer events target the scroller itself. Detect the real
 * scrollbar-side gutter, not the symmetric gutter reserved on the other side
 * by `scrollbar-gutter: stable both-edges`.
 */
export function isNativeVerticalScrollbarPointer(element: HTMLElement, pointer: ScrollbarPointer): boolean {
  if (pointer.button !== 0 || element.scrollHeight <= element.clientHeight + 1 || element.offsetWidth <= 0) return false;
  const rect = element.getBoundingClientRect();
  if (rect.width <= 0 || pointer.clientX < rect.left || pointer.clientX > rect.right) return false;

  const scaleX = rect.width / element.offsetWidth;
  const contentLeft = rect.left + element.clientLeft * scaleX;
  const contentRight = contentLeft + element.clientWidth * scaleX;
  const direction = element.ownerDocument.defaultView?.getComputedStyle(element).direction ?? "ltr";
  if (direction === "rtl") return contentLeft - rect.left > 1 && pointer.clientX < contentLeft;
  return rect.right - contentRight > 1 && pointer.clientX >= contentRight;
}

/** Keep a lazy Markdown source fallback from becoming an exact row size. */
export function hasPendingTranscriptGeometry(element: HTMLElement): boolean {
  return element.querySelector("[data-transcript-geometry-pending]") !== null;
}

/** Keep Virtuoso's current size tree stable while the native thumb owns it. */
export function measureTranscriptVirtuosoItem(
  element: Parameters<SizeFunction>[0],
  field: Parameters<SizeFunction>[1],
  freeze: boolean,
): number {
  // Freeze only rows whose async content is still pending geometry. Keeping
  // the freeze narrowly scoped avoids returning stale estimates for already
  // rendered rows, which would leave them visually misaligned after a manual
  // scroll/selection gesture ends (adapted from esengine#9366 by Linearl).
  if (freeze && field === "offsetHeight" && hasPendingTranscriptGeometry(element)) {
    const knownSize = Number.parseFloat(element.dataset.knownSize ?? "");
    if (Number.isFinite(knownSize) && knownSize > 0) return knownSize;
    const transcriptEstimate = Number.parseFloat(element.dataset.transcriptEstimate ?? "");
    if (Number.isFinite(transcriptEstimate) && transcriptEstimate > 0) return transcriptEstimate;
    const staticEstimate = Number.parseFloat(element.dataset.staticEstimate ?? "");
    if (Number.isFinite(staticEstimate) && staticEstimate > 0) return staticEstimate;
  }
  if (field === "offsetHeight" && hasPendingTranscriptGeometry(element)) {
    const estimate = Number.parseFloat(element.dataset.transcriptEstimate ?? element.dataset.staticEstimate ?? "");
    if (Number.isFinite(estimate) && estimate > 0) return estimate;
  }
  return Math.round(element.getBoundingClientRect()[field === "offsetWidth" ? "width" : "height"]);
}
