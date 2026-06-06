export interface UndoAppliedEvent {
  batchId: number;
  source: string;
  paths: string[];
}

export interface CodeUndoResult {
  info: string;
  contextEvent?: UndoAppliedEvent;
}

export type CodeUndoOutput = string | CodeUndoResult;

export function codeUndoInfo(result: CodeUndoOutput): string {
  return typeof result === "string" ? result : result.info;
}

export function formatUndoContextMessage(event: UndoAppliedEvent): string {
  const paths = event.paths.length === 1 ? event.paths[0] : event.paths.join(", ");
  return [
    "[Reasonix local state update: undo]",
    `The user ran /undo and reverted edit batch #${event.batchId} (${event.source}).`,
    `Reverted path(s): ${paths}.`,
    "Treat earlier messages or tool results that described those edits as stale. Re-read the files before relying on their current contents.",
  ].join("\n");
}

export function codeUndoContextMessage(result: CodeUndoOutput): string | undefined {
  if (typeof result === "string" || !result.contextEvent) return undefined;
  return formatUndoContextMessage(result.contextEvent);
}
