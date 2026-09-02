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

// Negative means pending confirmation; positive means confirmed reachable tail.
const nativeTranscriptTailResiduals = new WeakMap<object, number>();

/** Remember a small, synchronous native clamp after repeated tail writes.
 * WebView2 can expose a stable Virtuoso scrollHeight whose last few pixels are
 * not reachable through scrollTop. A single no-op can instead be Virtuoso
 * restoring a stale range, so require the same residual on stable geometry
 * before accepting it. Large gaps still use the LAST-item recovery path. */
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
  ) {
    nativeTranscriptTailResiduals.delete(element);
    return false;
  }
  const observed = nativeTranscriptTailResiduals.get(element);
  if (observed != null && Math.abs(observed) === residual) {
    nativeTranscriptTailResiduals.set(element, residual);
    return true;
  }
  nativeTranscriptTailResiduals.set(element, -residual);
  return false;
}

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return nativeTranscriptBottomTop(element) - element.scrollTop;
}

export function nativeTranscriptBottomTop(element: NativeTranscriptGeometry) {
  const theoreticalTop = tailTop(element);
  const residual = Math.max(0, nativeTranscriptTailResiduals.get(element) ?? 0);
  const observedTop = theoreticalTop - residual;
  if (element.scrollTop == null || element.scrollTop <= observedTop + TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) return observedTop;
  nativeTranscriptTailResiduals.delete(element);
  return theoreticalTop;
}

/** Explicit tail transactions always probe the theoretical native extent.
 * A confirmed WebView2 clamp remains the logical bottom if that write is a
 * no-op, but a browser that later accepts the last pixels clears the residual. */
export function tailTop(element: NativeTranscriptGeometry) {
  return Math.max(0, element.scrollHeight - element.clientHeight);
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
