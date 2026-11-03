// Why a held queue entry stopped, keyed by the kernel's block code. A code with
// no entry shows the kernel's own diagnostic.
export const BLOCK_WHY: Record<string, string> = {
  steer_unapplied: "queueWhy.thisGuidanceWasNotDelivered",
  turn_snapshot_failed: "queueWhy.theTurnEndedButThe",
  turn_ack_failed: "queueWhy.theTurnEndedButThe2",
  owner_inactive: "queueWhy.theSessionThatWasRunning",
  manifest_salvaged: "queueWhy.theQueueRecordWasCorrupt",
};
