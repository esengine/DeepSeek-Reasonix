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

/** Keep Virtuoso's current size tree stable while the native thumb owns it. */
export function measureTranscriptVirtuosoItem(
  element: Parameters<SizeFunction>[0],
  field: Parameters<SizeFunction>[1],
  freeze: boolean,
): number {
  if (freeze) {
    const knownSize = Number.parseFloat(element.dataset.knownSize ?? "");
    if (Number.isFinite(knownSize) && knownSize > 0) return knownSize;
  }
  return Math.round(element.getBoundingClientRect()[field === "offsetWidth" ? "width" : "height"]);
}
