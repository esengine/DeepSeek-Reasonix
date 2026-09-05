import { useLayoutEffect, useMemo } from "react";
import { useCommittedSlot, type CommittedSlot } from "./useCommittedSlot";
import { createPendingRevisionOwner, type PendingRevisionInput } from "../app-runtime/pendingRevisionOwner";

function bindOwner(slot: CommittedSlot<PendingRevisionInput>) {
  return createPendingRevisionOwner(() => slot.phase === "ready" && slot.value ? { epoch: slot.epoch, input: slot.value } : undefined);
}
export function reportPendingRevisionFailure(error: unknown) {
  console.warn("Failed to submit pending plan revision", error);
}

export function usePendingPlanRevisions(input: PendingRevisionInput) {
  const slot = useCommittedSlot(input);
  const owner = useMemo(() => bindOwner(slot), [slot]);
  useLayoutEffect(() => { owner.pump(); });
  useLayoutEffect(() => () => owner.dispose(), [owner]);
  return owner.remember;
}
