export const DEFAULT_COMPOSER_ROWS = 5;
export const MAX_COMPOSER_ROWS = 15;

export type ComposerTextareaSizing = {
  heightPx: number;
  overflowY: "hidden" | "auto";
};

export function getComposerTextareaSizing({
  contentRows,
  lineHeightPx,
  verticalPaddingPx,
}: {
  contentRows: number;
  lineHeightPx: number;
  verticalPaddingPx: number;
}): ComposerTextareaSizing {
  const safeRows = Math.max(1, Math.ceil(contentRows));
  const visibleRows = Math.min(Math.max(safeRows, DEFAULT_COMPOSER_ROWS), MAX_COMPOSER_ROWS);

  return {
    heightPx: visibleRows * lineHeightPx + verticalPaddingPx,
    overflowY: safeRows > MAX_COMPOSER_ROWS ? "auto" : "hidden",
  };
}

export function applyComposerTextareaAutosize(textarea: HTMLTextAreaElement) {
  const style = window.getComputedStyle(textarea);
  const lineHeightPx = Number.parseFloat(style.lineHeight);
  const paddingTopPx = Number.parseFloat(style.paddingTop);
  const paddingBottomPx = Number.parseFloat(style.paddingBottom);
  const verticalPaddingPx = paddingTopPx + paddingBottomPx;
  const measuredLineHeight = Number.isFinite(lineHeightPx) ? lineHeightPx : 20;

  textarea.style.height = "auto";
  const contentRows = Math.ceil(
    Math.max(textarea.scrollHeight - verticalPaddingPx, measuredLineHeight) / measuredLineHeight,
  );
  const sizing = getComposerTextareaSizing({
    contentRows,
    lineHeightPx: measuredLineHeight,
    verticalPaddingPx,
  });
  textarea.style.height = `${sizing.heightPx}px`;
  textarea.style.overflowY = sizing.overflowY;
}
