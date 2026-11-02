import { useSyncExternalStore } from "react";

// Which group's chat fills the main area, if any. A group is not a pane, so the
// window's pane list cannot say; this one value does, and opening a pane clears
// it.
let group: string | null = null;
const listeners = new Set<() => void>();

function set(next: string | null) {
  group = next;
  for (const l of listeners) l();
}

export function subscribeGroupStage(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export const openGroupStage = (id: string) => set(id);
export const closeGroupStage = () => set(null);

export function useGroupStage(): string | null {
  return useSyncExternalStore(
    subscribeGroupStage,
    () => group,
  );
}
