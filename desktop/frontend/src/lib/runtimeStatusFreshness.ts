type RuntimeStatusState = {
  runtimeStatusEpoch?: string;
  runtimeStatusSeq?: number;
  runtimeStatusSnapshotAt?: number;
};
type RuntimeStatusSnapshot = {
  runtimeEpoch?: string;
  turnEventSeq?: number;
  snapshotAt?: number;
};

export function runtimeStatusSnapshotIsStale(state: RuntimeStatusState, snapshot: RuntimeStatusSnapshot): boolean {
  const incomingEpoch = snapshot.runtimeEpoch?.trim();
  const storedEpoch = state.runtimeStatusEpoch?.trim();
  if (incomingEpoch && storedEpoch && incomingEpoch !== storedEpoch) return false;
  if (snapshot.turnEventSeq === undefined || state.runtimeStatusSeq === undefined) return false;
  if (snapshot.turnEventSeq < state.runtimeStatusSeq) return true;
  // A rejected optimistic send need not append a backend event. A fresh read
  // at the same sequence can reconcile it; duplicate and out-of-order reads
  // cannot override a more recent snapshot.
  return snapshot.turnEventSeq === state.runtimeStatusSeq && (snapshot.snapshotAt === undefined ||
    (state.runtimeStatusSnapshotAt !== undefined && snapshot.snapshotAt <= state.runtimeStatusSnapshotAt));
}
