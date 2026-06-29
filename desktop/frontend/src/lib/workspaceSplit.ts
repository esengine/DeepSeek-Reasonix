export function clampWorkspaceSplitTreeWidth({
  width,
  panelWidth,
  railWidth = 0,
  treeMinWidth,
  previewMinWidth,
}: {
  width: number;
  panelWidth?: number;
  railWidth?: number;
  treeMinWidth: number;
  previewMinWidth: number;
}): number {
  const min = Math.round(treeMinWidth);
  const rounded = Math.round(width);
  if (typeof panelWidth !== "number" || !Number.isFinite(panelWidth)) {
    return Math.max(min, rounded);
  }
  const splitWidth = Math.max(0, Math.round(panelWidth) - Math.round(railWidth));
  const max = Math.max(min, splitWidth - Math.round(previewMinWidth));
  return Math.min(max, Math.max(min, rounded));
}

export function initialWorkspaceSplitTreeWidth({
  panelWidth,
  railWidth = 0,
  savedTreeWidth,
  treeMinWidth,
  previewMinWidth,
}: {
  panelWidth?: number;
  railWidth?: number;
  savedTreeWidth: number | null;
  treeMinWidth: number;
  previewMinWidth: number;
}): number {
  const target =
    savedTreeWidth !== null && Number.isFinite(savedTreeWidth)
      ? savedTreeWidth
      : typeof panelWidth === "number" && Number.isFinite(panelWidth)
        ? Math.max(0, panelWidth - railWidth) / 2
        : treeMinWidth;
  return clampWorkspaceSplitTreeWidth({ width: target, panelWidth, railWidth, treeMinWidth, previewMinWidth });
}

export type WorkspaceSplitTreeWidthMode = "even" | "manual";

export function shouldInitializeWorkspaceSplitOnFileSelect({
  previewVisible,
  treeVisible,
}: {
  previewVisible: boolean;
  treeVisible: boolean;
}): boolean {
  return !previewVisible || !treeVisible;
}

export function resolveWorkspaceSplitTreeWidth({
  mode,
  currentTreeWidth,
  panelWidth,
  railWidth = 0,
  treeMinWidth,
  previewMinWidth,
}: {
  mode: WorkspaceSplitTreeWidthMode;
  currentTreeWidth: number;
  panelWidth?: number;
  railWidth?: number;
  treeMinWidth: number;
  previewMinWidth: number;
}): number {
  if (mode === "even") {
    return initialWorkspaceSplitTreeWidth({
      panelWidth,
      railWidth,
      savedTreeWidth: null,
      treeMinWidth,
      previewMinWidth,
    });
  }
  return clampWorkspaceSplitTreeWidth({
    width: currentTreeWidth,
    panelWidth,
    railWidth,
    treeMinWidth,
    previewMinWidth,
  });
}

export function workspaceSplitCanFit({
  panelWidth,
  railWidth = 0,
  treeMinWidth,
  previewMinWidth,
}: {
  panelWidth?: number;
  railWidth?: number;
  treeMinWidth: number;
  previewMinWidth: number;
}): boolean {
  if (typeof panelWidth !== "number" || !Number.isFinite(panelWidth)) {
    return true;
  }
  return Math.round(panelWidth) >= Math.round(railWidth) + Math.round(treeMinWidth) + Math.round(previewMinWidth);
}

export function workspaceSplitTreeWidthFromPointer({
  clientX,
  panelLeft,
  panelWidth,
  railWidth = 0,
  treeMinWidth,
  previewMinWidth,
}: {
  clientX: number;
  panelLeft: number;
  panelWidth?: number;
  railWidth?: number;
  treeMinWidth: number;
  previewMinWidth: number;
}): number {
  return clampWorkspaceSplitTreeWidth({
    width: clientX - panelLeft - railWidth,
    panelWidth,
    railWidth,
    treeMinWidth,
    previewMinWidth,
  });
}
