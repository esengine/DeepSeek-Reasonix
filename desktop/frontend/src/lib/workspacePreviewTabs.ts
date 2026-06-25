export type WorkspacePreviewTabsState = {
  openTabs: string[];
  selectedPath: string | null;
};

export function closeWorkspacePreviewTab(openTabs: string[], selectedPath: string | null): WorkspacePreviewTabsState {
  const nextOpenTabs = selectedPath ? openTabs.filter((tab) => tab !== selectedPath) : openTabs;
  return {
    openTabs: nextOpenTabs,
    selectedPath: nextOpenTabs.length > 0 ? nextOpenTabs[nextOpenTabs.length - 1] : null,
  };
}
