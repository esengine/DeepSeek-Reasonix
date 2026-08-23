export type RuntimeMetaSnapshot = {
  running: boolean;
  turnStartedAt?: number;
  pendingPrompt?: boolean;
  backgroundJobs?: number;
  cancelRequested?: boolean;
  cancellable?: boolean;
};

export function foregroundRunningFromRuntimeMeta(meta: RuntimeMetaSnapshot): boolean {
  if (typeof meta.cancellable === "boolean") return meta.cancellable;
  if ((meta.backgroundJobs ?? 0) > 0 && !meta.pendingPrompt) return false;
  return Boolean(meta.running);
}
