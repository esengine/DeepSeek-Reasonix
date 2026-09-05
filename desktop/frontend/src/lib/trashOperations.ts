export type TrashOperationSummary = { succeeded: string[]; failed: string[] };
export async function purgeTrashBatch(paths: string[], purge: (path: string) => Promise<void>): Promise<TrashOperationSummary> {
  const result: TrashOperationSummary = { succeeded: [], failed: [] };
  for (const path of new Set(paths)) {
    try { await purge(path); result.succeeded.push(path); }
    catch { result.failed.push(path); }
  }
  return result;
}
