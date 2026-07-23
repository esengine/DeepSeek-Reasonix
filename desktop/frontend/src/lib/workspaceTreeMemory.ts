export interface WorkspaceTreeMemorySnapshot {
  openDirs: Set<string>;
  visitId: number;
}

const workspaceTreeMemory = new Map<string, WorkspaceTreeMemorySnapshot>();
let activeWorkspaceTreeKey = "";
let workspaceTreeVisitSequence = 0;

export function workspaceTreeVisitId(memoryKey: string): number {
  if (activeWorkspaceTreeKey !== memoryKey) {
    activeWorkspaceTreeKey = memoryKey;
    workspaceTreeVisitSequence += 1;
  }
  return workspaceTreeVisitSequence;
}

export function readWorkspaceTreeMemory(memoryKey: string): WorkspaceTreeMemorySnapshot | null {
  const snapshot = workspaceTreeMemory.get(memoryKey);
  if (!snapshot) return null;
  return {
    openDirs: new Set(snapshot.openDirs),
    visitId: snapshot.visitId,
  };
}

export function rememberWorkspaceTreeOpenDirs(memoryKey: string, openDirs: ReadonlySet<string>, visitId: number): void {
  workspaceTreeMemory.set(memoryKey, {
    openDirs: new Set(openDirs),
    visitId,
  });
}

export function touchWorkspaceTreeVisit(memoryKey: string, visitId: number): void {
  const snapshot = workspaceTreeMemory.get(memoryKey);
  workspaceTreeMemory.set(memoryKey, {
    openDirs: new Set(snapshot?.openDirs ?? [""]),
    visitId,
  });
}

export function resetWorkspaceTreeMemoryForTests(): void {
  workspaceTreeMemory.clear();
  activeWorkspaceTreeKey = "";
  workspaceTreeVisitSequence = 0;
}
