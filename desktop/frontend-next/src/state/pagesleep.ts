import { useSyncExternalStore } from "react";

// Whether pages nobody has driven for a while are put to sleep whole, and woken
// as the same tab when the agent next speaks to them. It is local to this
// machine, never read from project settings.
export type PageSleep = "off" | "on";
const KEY = "rx-page-sleep";
const listeners = new Set<() => void>();
let transient: PageSleep = "off";
let storageFailed = false;

export function pageSleep(): PageSleep {
  if (storageFailed) return transient;
  try {
    return localStorage.getItem(KEY) === "on" ? "on" : "off";
  } catch {
    return transient;
  }
}

export function setPageSleep(next: PageSleep): void {
  transient = next;
  try {
    localStorage.setItem(KEY, next);
    storageFailed = false;
  } catch {
    storageFailed = true;
  }
  listeners.forEach((fn) => fn());
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  const storage = (event: StorageEvent) => {
    if (event.key === KEY || event.key === null) fn();
  };
  window.addEventListener("storage", storage);
  return () => {
    listeners.delete(fn);
    window.removeEventListener("storage", storage);
  };
}

export function usePageSleep(): PageSleep {
  return useSyncExternalStore(subscribe, pageSleep, pageSleep);
}
