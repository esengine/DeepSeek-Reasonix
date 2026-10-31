import { useSyncExternalStore } from "react";

// How much the agents move, on this machine. Quiet keeps only what says a
// state (working, waiting on the person); normal adds the answer to a pointer
// and rows sliding into a new place; lively adds idle blinks and fidgets. The
// system's reduced-motion setting stops all of it in the stylesheet.
export type TeamMotion = "quiet" | "normal" | "lively";

const KEY = "rx-team-motion";
const listeners = new Set<() => void>();

function read(): TeamMotion {
  try {
    const v = localStorage.getItem(KEY);
    return v === "quiet" || v === "lively" ? v : "normal";
  } catch {
    return "normal";
  }
}

let motion: TeamMotion = read();

function apply() {
  if (typeof document !== "undefined") document.documentElement.dataset.teamMotion = motion;
}
apply();

export function setTeamMotion(next: TeamMotion) {
  try {
    localStorage.setItem(KEY, next);
  } catch {
    /* a private window keeps the choice for this page only */
  }
  motion = next;
  apply();
  for (const l of listeners) l();
}

export function teamMotion(): TeamMotion {
  return motion;
}

export function useTeamMotion(): TeamMotion {
  return useSyncExternalStore((l) => {
    listeners.add(l);
    return () => void listeners.delete(l);
  }, teamMotion);
}
