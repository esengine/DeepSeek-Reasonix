/** Minimum thread width in pixels to ensure readability */
const MIN_THREAD_WIDTH = 580;

/** Right margin in pixels to prevent thread from touching the edge */
const THREAD_MARGIN = 80;

/** Maximum thread width in pixels to maintain optimal line length for reading */
const MAX_THREAD_WIDTH = 1120;

export function getThreadMaxWidth({
  viewportWidth,
  visibleSide,
  visibleCtx,
}: {
  viewportWidth: number;
  visibleSide: number;
  visibleCtx: number;
}): number {
  return Math.max(MIN_THREAD_WIDTH, Math.min(viewportWidth - visibleSide - visibleCtx - THREAD_MARGIN, MAX_THREAD_WIDTH));
}
