import { isTranscriptContentShrink } from "./transcriptScrollArbiter";

export const TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX = 4;

export type TranscriptFollowGeometry = {
  contentExtent: number | null;
  viewportExtent: number | null;
};

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return element.scrollHeight - element.scrollTop - element.clientHeight;
}

export function nativeTranscriptBottomTop(element: { scrollHeight: number; clientHeight: number }) {
  return Math.max(0, element.scrollHeight - element.clientHeight);
}

export function hasTranscriptScrollableRange(
  element: { scrollHeight: number; clientHeight: number },
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
) {
  return nativeTranscriptBottomTop(element) > threshold;
}

/** Returns true when the scroller was off-bottom and is now pinned. */
export function pinTranscriptScrollerToNativeTail(
  element: { scrollHeight: number; scrollTop: number; clientHeight: number },
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
): boolean {
  const bottom = nativeTranscriptBottomTop(element);
  if (bottom - element.scrollTop <= threshold) return false;
  element.scrollTop = bottom;
  return true;
}

export function pinTranscriptTailAfterViewportShrink(
  element: { scrollHeight: number; scrollTop: number; clientHeight: number },
  geometry: TranscriptFollowGeometry,
  tailFollow: boolean,
): number | null {
  const viewport = element.clientHeight;
  const viewportShrunk = geometry.viewportExtent != null
    && geometry.viewportExtent - viewport > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
  geometry.viewportExtent = viewport;
  const contentShrunk = geometry.contentExtent != null
    && isTranscriptContentShrink(element.scrollHeight - geometry.contentExtent);
  if (!tailFollow || !viewportShrunk || contentShrunk) return null;
  return pinTranscriptScrollerToNativeTail(element)
    ? nativeTranscriptBottomTop(element)
    : null;
}
