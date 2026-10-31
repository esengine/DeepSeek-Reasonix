import { useState, useSyncExternalStore } from "react";

const KEY = "rx-sidebar-folds";

function read(): Set<string> {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    return new Set(Array.isArray(saved) ? saved.filter((key): key is string => typeof key === "string") : []);
  } catch {
    return new Set();
  }
}

export function useSidebarFolds(): [Set<string>, (next: Set<string>) => void] {
  const [folded, setFolded] = useState(read);
  const save = (next: Set<string>) => {
    setFolded(next);
    try {
      localStorage.setItem(KEY, JSON.stringify([...next]));
    } catch {
      // Folding remains available when this window cannot persist its choices.
    }
  };
  return [folded, save];
}

const PROJECTS_KEY = "rx-projects-folded";
const projectListeners = new Set<() => void>();
let volatileProjectsFolded: boolean | undefined;

function projectsFolded(): boolean {
  if (volatileProjectsFolded !== undefined) return volatileProjectsFolded;
  try { return localStorage.getItem(PROJECTS_KEY) === "1"; } catch { return false; }
}

export function setProjectsFolded(next: boolean) {
  try {
    localStorage.setItem(PROJECTS_KEY, next ? "1" : "0");
    volatileProjectsFolded = undefined;
  } catch {
    // A private window still keeps the choice until the page closes.
    volatileProjectsFolded = next;
  }
  for (const listener of projectListeners) listener();
}

/** Navigation and the heading share the section's visibility owner. */
export function useProjectsFolded(): [boolean, (next: boolean) => void] {
  const folded = useSyncExternalStore((listener) => {
    projectListeners.add(listener);
    return () => { projectListeners.delete(listener); };
  }, projectsFolded);
  return [folded, setProjectsFolded];
}
