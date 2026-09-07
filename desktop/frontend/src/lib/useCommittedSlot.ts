import { useLayoutEffect, useRef } from "react";

export type CommittedSlot<Value> = {
  value?: Value;
  epoch: number;
  phase: "not-ready" | "ready" | "disposed";
  requestId: number;
};

/** Commit is the only publication boundary; cleanup synchronously revokes it. */
export function useCommittedSlot<Value>(value: Value): CommittedSlot<Value> {
  const slotRef = useRef<CommittedSlot<Value>>({ epoch: 0, phase: "not-ready", requestId: 0 });
  const slot = slotRef.current;
  useLayoutEffect(() => {
    if (slot.phase !== "ready") slot.epoch += 1;
    slot.phase = "ready";
    slot.value = value;
  });
  useLayoutEffect(() => () => {
    slot.phase = "disposed";
    slot.value = undefined;
  }, [slot]);
  return slot;
}
