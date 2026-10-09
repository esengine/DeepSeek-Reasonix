import { useSyncExternalStore } from "react";

// Which remote machines this window keeps out of the sidebar. A display choice
// and nothing more: the host book still holds them, the settings page still
// lists them, and connecting to one still works — only the rail row is gone.
const KEY = "rx-hidden-remote-hosts";
let raw: string | null | undefined;
let value: readonly string[] = [];
const listeners = new Set<() => void>();

export function normalizeHiddenHosts(input: unknown): string[] {
  if (!Array.isArray(input)) return [];
  return [...new Set(input.filter((item): item is string => typeof item === "string" && item.trim() !== "").map((item) => item.trim()))];
}

export function readHiddenHosts(): readonly string[] {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(KEY);
  } catch {
    /* storage is unavailable; nothing is hidden */
  }
  if (stored !== raw) {
    raw = stored;
    try {
      value = normalizeHiddenHosts(JSON.parse(stored ?? "[]"));
    } catch {
      value = [];
    }
  }
  return value;
}

export function writeHiddenHosts(names: readonly string[]): boolean {
  const next = normalizeHiddenHosts(names);
  const stored = JSON.stringify(next);
  try {
    localStorage.setItem(KEY, stored);
  } catch {
    return false;
  }
  if (stored !== raw) {
    raw = stored;
    value = next;
    listeners.forEach((fn) => fn());
  }
  return true;
}

export function toggleHiddenHost(name: string): void {
  const current = readHiddenHosts();
  writeHiddenHosts(current.includes(name) ? current.filter((one) => one !== name) : [...current, name]);
}

function refresh(event: StorageEvent): void {
  if (event.key !== KEY && event.key !== null) return;
  const before = raw;
  raw = undefined;
  readHiddenHosts();
  if (raw !== before) listeners.forEach((fn) => fn());
}

export function onHiddenHostsChange(fn: () => void): () => void {
  listeners.add(fn);
  if (listeners.size === 1) window.addEventListener("storage", refresh);
  return () => {
    listeners.delete(fn);
    if (listeners.size === 0) window.removeEventListener("storage", refresh);
  };
}

export function useHiddenHosts(): readonly string[] {
  return useSyncExternalStore(onHiddenHostsChange, readHiddenHosts, readHiddenHosts);
}
