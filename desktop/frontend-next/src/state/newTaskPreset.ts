import { useSyncExternalStore } from "react";

export const NEW_TASK_PRESET_KEY = "rx-new-task-agent-preset";
const listeners = new Set<() => void>();
// A built-in preset, or one of the person's own: a project's shared preset is
// not offered, because it exists only where that project is open.
type NewTaskPresetId = "builtin:general" | "builtin:minimal" | `user:${string}`;
const valid = (value: unknown): value is NewTaskPresetId =>
  value === "builtin:general" || value === "builtin:minimal" || (typeof value === "string" && /^user:[\w.-]+$/.test(value));

export function newTaskPreset(): NewTaskPresetId | undefined {
  try {
    const value = localStorage.getItem(NEW_TASK_PRESET_KEY);
    return valid(value) ? value : undefined;
  } catch { return undefined; }
}

export function setNewTaskPreset(id: string): boolean {
  if (!valid(id)) return false;
  try { localStorage.setItem(NEW_TASK_PRESET_KEY, id); }
  catch { return false; }
  listeners.forEach(fn => fn());
  return true;
}

export function forgetNewTaskPreset() {
  try { localStorage.removeItem(NEW_TASK_PRESET_KEY); } catch { return; }
  listeners.forEach(fn => fn());
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  const refresh = (event: StorageEvent) => {
    if (event.key === NEW_TASK_PRESET_KEY || event.key === null) fn();
  };
  window.addEventListener("storage", refresh);
  return () => { listeners.delete(fn); window.removeEventListener("storage", refresh); };
}

export const useNewTaskPreset = () => useSyncExternalStore(subscribe, newTaskPreset, newTaskPreset);

export function newTaskComposition(explicit?: string, sessionPath?: string): string | undefined {
  return explicit !== undefined || sessionPath ? explicit : newTaskPreset();
}
