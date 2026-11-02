import { useSyncExternalStore } from "react";

// Whether the focused agent's details are drawn beside its conversation. One
// window-wide answer: moving from one agent to another keeps the details open
// and shows the other's.
let open = false;
const listeners = new Set<() => void>();

function set(next: boolean) {
  if (next === open) return;
  open = next;
  for (const l of listeners) l();
}

export const toggleSeatDrawer = () => set(!open);
export const closeSeatDrawer = () => set(false);

export function useSeatDrawer(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => void listeners.delete(l);
    },
    () => open,
  );
}
