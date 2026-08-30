import { isTranscriptContentShrink } from "./transcriptScrollArbiter";

export const TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX = 4;

export type TranscriptFollowGeometry = {
  contentExtent: number | null;
  viewportExtent: number | null;
};

type NativeTranscriptGeometry = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop?: number;
};

const observedNativeTranscriptTailResiduals = new WeakMap<object, number>();

/** Remember a small, synchronous native clamp after an explicit tail write.
 * WebView2 can expose a stable Virtuoso scrollHeight whose last few pixels are
 * not reachable through scrollTop. Large gaps are still treated as stale
 * virtual ranges and must go through the LAST-item recovery path. */
export function observeNativeTranscriptTailClamp(
  element: NativeTranscriptGeometry & { scrollTop: number },
  previousTop: number,
): boolean {
  const theoreticalTop = Math.max(0, element.scrollHeight - element.clientHeight);
  const residual = theoreticalTop - element.scrollTop;
  if (
    Math.abs(element.scrollTop - previousTop) > 0.5
    || residual <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    || residual > 64
  ) return false;
  observedNativeTranscriptTailResiduals.set(element, residual);
  return true;
}

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return nativeTranscriptBottomTop(element) - element.scrollTop;
}

export function nativeTranscriptBottomTop(element: NativeTranscriptGeometry) {
  const theoreticalTop = Math.max(0, element.scrollHeight - element.clientHeight);
  const residual = observedNativeTranscriptTailResiduals.get(element) ?? 0;
  const observedTop = theoreticalTop - residual;
  if (element.scrollTop == null || element.scrollTop <= observedTop + TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) return observedTop;
  observedNativeTranscriptTailResiduals.delete(element);
  return theoreticalTop;
}

export function hasTranscriptScrollableRange(
  element: NativeTranscriptGeometry,
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
) {
  return nativeTranscriptBottomTop(element) > threshold;
}

export function pinTranscriptTailAfterViewportShrink(
  element: { scrollHeight: number; scrollTop: number; clientHeight: number },
  geometry: TranscriptFollowGeometry,
  tailFollow: boolean,
): number | null {
  const viewport = Math.max(0, element.clientHeight);
  const viewportShrunk = geometry.viewportExtent != null
    && geometry.viewportExtent - viewport > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
  geometry.viewportExtent = viewport;
  const contentShrunk = geometry.contentExtent != null
    && isTranscriptContentShrink(element.scrollHeight - geometry.contentExtent);
  if (!tailFollow || !viewportShrunk || contentShrunk) return null;
  const bottom = nativeTranscriptBottomTop(element);
  return bottom - element.scrollTop > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX ? bottom : null;
}
