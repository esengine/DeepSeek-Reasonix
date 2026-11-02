import { useSyncExternalStore } from "react";

// Whether the team's block is folded in the sidebar, on this machine.
const KEY = "rx-team-folded";
const listeners = new Set<() => void>();

function read(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

let folded = read();

export function setTeamFolded(next: boolean) {
  try {
    localStorage.setItem(KEY, next ? "1" : "0");
  } catch {
    /* a private window keeps the choice for this page only */
  }
  folded = next;
  for (const l of listeners) l();
}

export function useTeamFolded(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => folded,
  );
}
