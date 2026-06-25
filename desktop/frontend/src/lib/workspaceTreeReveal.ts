export function shouldScrollWorkspaceTreeSelection({
  selectedPath,
  pendingRevealPath,
  actualTreeVisible,
  selectedIndex,
}: {
  selectedPath: string | null;
  pendingRevealPath: string | null;
  actualTreeVisible: boolean;
  selectedIndex: number;
}): boolean {
  return Boolean(
    selectedPath &&
      pendingRevealPath === selectedPath &&
      actualTreeVisible &&
      selectedIndex >= 0,
  );
}
