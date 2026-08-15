export function availableWorkspacePanelWidth({
  viewportWidth,
  sidebarCollapsed,
  sidebarWidth,
  chatMinWidth,
  resizerWidth,
}: {
  viewportWidth: number;
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  chatMinWidth: number;
  resizerWidth: number;
}): number {
  return Math.max(0, viewportWidth - (sidebarCollapsed ? 0 : sidebarWidth) - chatMinWidth - resizerWidth);
}

export function resolveWorkspacePanelWidth({
  open,
  maximized,
  preferredWidth,
  minWidth,
  availableWidth,
}: {
  open: boolean;
  maximized: boolean;
  preferredWidth: number;
  minWidth: number;
  availableWidth: number;
}): number {
  if (!open || maximized) return preferredWidth;
  return Math.min(Math.max(minWidth, preferredWidth), Math.max(0, availableWidth));
}

export function resolveLiveWorkspacePanelWidth({
  viewportWidth,
  sidebarCollapsed,
  sidebarWidth,
  chatMinWidth,
  resizerWidth,
  open,
  maximized,
  preferredWidth,
  minWidth,
}: {
  viewportWidth: number;
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  chatMinWidth: number;
  resizerWidth: number;
  open: boolean;
  maximized: boolean;
  preferredWidth: number;
  minWidth: number;
}): number {
  return resolveWorkspacePanelWidth({
    open,
    maximized,
    preferredWidth,
    minWidth,
    availableWidth: availableWorkspacePanelWidth({
      viewportWidth,
      sidebarCollapsed,
      sidebarWidth,
      chatMinWidth,
      resizerWidth,
    }),
  });
}

export function workspacePanelAriaMinWidth(minWidth: number, renderedWidth: number): number {
  return Math.min(minWidth, renderedWidth);
}

export function resolveWorkspacePanelPlacement({
  viewportWidth, sidebarCollapsed, sidebarWidth, chatMinWidth, resizerWidth,
  open, maximized, preferredWidth, minWidth, minRenderWidth, liveWidth,
}: {
  viewportWidth: number; sidebarCollapsed: boolean; sidebarWidth: number;
  chatMinWidth: number; resizerWidth: number; open: boolean; maximized: boolean;
  preferredWidth: number; minWidth: number; minRenderWidth: number; liveWidth?: number | null;
}) {
  const resolvedWidth = resolveLiveWorkspacePanelWidth({
    viewportWidth, sidebarCollapsed, sidebarWidth, chatMinWidth, resizerWidth,
    open, maximized, preferredWidth, minWidth,
  });
  const overlay = open && !maximized && resolvedWidth < minRenderWidth;
  const storedWidth = maximized
    ? preferredWidth
    : overlay ? Math.min(preferredWidth, Math.max(minWidth, viewportWidth - 16)) : resolvedWidth;
  const renderWidth = liveWidth ?? storedWidth;
  const renderable = open && (maximized || overlay || renderWidth >= minRenderWidth);
  return { renderWidth, overlay, renderable, gridOpen: renderable && !maximized && !overlay };
}
