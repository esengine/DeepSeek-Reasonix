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

/** Pending async Markdown keeps its seed; resolved rows always report reality. */
export function measureTranscriptVirtuosoItem(
  element: Parameters<SizeFunction>[0],
  field: Parameters<SizeFunction>[1],
): number {
  if (field === "offsetHeight" && hasPendingTranscriptGeometry(element)) {
    const estimate = Number.parseFloat(element.dataset.transcriptEstimate ?? element.dataset.staticEstimate ?? "");
    if (Number.isFinite(estimate) && estimate > 0) return estimate;
  }
  return Math.round(element.getBoundingClientRect()[field === "offsetWidth" ? "width" : "height"]);
}
