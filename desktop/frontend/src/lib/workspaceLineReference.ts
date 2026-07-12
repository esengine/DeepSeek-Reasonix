export interface MonacoSelectionLike {
  startLineNumber: number;
  startColumn: number;
  endLineNumber: number;
  endColumn: number;
}

export interface WorkspaceLineRange {
  startLine: number;
  endLine: number;
}

export function normalizeMonacoSelectionRange(selection: MonacoSelectionLike | null | undefined): WorkspaceLineRange | null {
  if (!selection) return null;
  let startLine = Math.min(selection.startLineNumber, selection.endLineNumber);
  let endLine = Math.max(selection.startLineNumber, selection.endLineNumber);
  const empty =
    selection.startLineNumber === selection.endLineNumber &&
    selection.startColumn === selection.endColumn;
  if (empty) return null;

  const maxLineColumn =
    selection.startLineNumber > selection.endLineNumber
      ? selection.startColumn
      : selection.endColumn;
  if (maxLineColumn === 1 && endLine > startLine) {
    endLine -= 1;
  }
  if (endLine < startLine) return null;
  return { startLine, endLine };
}

export function formatWorkspaceLineReference(path: string, startLine: number, endLine: number): string {
  const start = Math.max(1, Math.floor(startLine));
  const end = Math.max(start, Math.floor(endLine));
  return start === end ? `@${path}:${start}` : `@${path}:${start}-${end}`;
}
