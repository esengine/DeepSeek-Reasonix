export interface ComposerContentSizing {
  inputHeight: number;
  logicalHeight: number | null;
  overflow: boolean;
}

export function resolveComposerContentSizing({
  contentHeight,
  manualLogicalHeight,
  maxLogicalHeight,
  reservedHeight,
  minInputHeight = 32,
}: {
  contentHeight: number;
  manualLogicalHeight: number | null;
  maxLogicalHeight: number;
  reservedHeight: number;
  minInputHeight?: number;
}): ComposerContentSizing {
  const safeContentHeight = Math.max(0, contentHeight);
  const maxInputHeight = Math.max(minInputHeight, maxLogicalHeight - reservedHeight);
  const manualInputHeight = manualLogicalHeight === null
    ? 0
    : Math.max(minInputHeight, manualLogicalHeight - reservedHeight);
  const inputHeight = Math.min(Math.max(safeContentHeight, manualInputHeight), maxInputHeight);
  const logicalHeight = manualLogicalHeight === null
    ? null
    : Math.min(maxLogicalHeight, Math.max(manualLogicalHeight, inputHeight + reservedHeight));

  return {
    inputHeight,
    logicalHeight,
    overflow: safeContentHeight > maxInputHeight + 1,
  };
}
