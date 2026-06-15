export const WORKSPACE_REF_DRAG_TYPE = "application/x-reasonix-workspace-ref";

export interface WorkspaceRefDragPayload {
  path: string;
  isDir?: boolean;
}

export function formatWorkspaceReference(path: string, isDir?: boolean): string {
  const clean = isDir && !path.endsWith("/") ? path + "/" : path;
  return `@${clean}`;
}

export function parseWorkspaceReference(text: string): WorkspaceRefDragPayload | null {
  const trimmed = text.trim();
  const match = /^@(\S+)$/.exec(trimmed);
  if (!match) return null;
  const path = match[1];
  if (!path) return null;
  return { path, isDir: path.endsWith("/") };
}

export function readWorkspaceReferenceDrag(dataTransfer: DataTransfer): WorkspaceRefDragPayload | null {
  if (!Array.from(dataTransfer.types).includes(WORKSPACE_REF_DRAG_TYPE)) return null;
  try {
    const payload = JSON.parse(dataTransfer.getData(WORKSPACE_REF_DRAG_TYPE)) as WorkspaceRefDragPayload;
    if (!payload.path) return null;
    return { path: payload.path, isDir: payload.isDir };
  } catch {
    return null;
  }
}
