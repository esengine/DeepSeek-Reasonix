export interface WindowRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PersistedBoundsSource {
  getNormalBounds(): WindowRect;
}

// Query normal-state bounds rather than the maximized outer frame. MainWindow
// retains the last non-minimized snapshot while iconic because some native
// implementations return the maximized frame from getNormalBounds then.
export function persistedWindowRect(win: PersistedBoundsSource): WindowRect {
  return win.getNormalBounds();
}

// All rectangles are Electron DIP coordinates. Display origins may be negative.
// The caller selects the matching display; this function only fits the rectangle.
export function restoreWindowRect(
  size: { width: number; height: number; minWidth: number; minHeight: number },
  position: { x: number; y: number } | undefined,
  workArea: WindowRect,
): WindowRect {
  const width = Math.min(Math.max(Math.round(size.width), size.minWidth), workArea.width);
  const height = Math.min(Math.max(Math.round(size.height), size.minHeight), workArea.height);
  const intersects = position && position.x < workArea.x + workArea.width &&
    position.y < workArea.y + workArea.height &&
    position.x + size.width > workArea.x && position.y + size.height > workArea.y;
  const x = intersects ? position.x : workArea.x + Math.floor((workArea.width - width) / 2);
  const y = intersects ? position.y : workArea.y + Math.floor((workArea.height - height) / 2);
  return {
    x: Math.round(Math.max(workArea.x, Math.min(x, workArea.x + workArea.width - width))),
    y: Math.round(Math.max(workArea.y, Math.min(y, workArea.y + workArea.height - height))),
    width,
    height,
  };
}
