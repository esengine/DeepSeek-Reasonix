export function getThreadMaxWidth({
  viewportWidth,
  visibleSide,
  visibleCtx,
}: {
  viewportWidth: number;
  visibleSide: number;
  visibleCtx: number;
}): number {
  return Math.max(580, Math.min(viewportWidth - visibleSide - visibleCtx - 80, 1120));
}
