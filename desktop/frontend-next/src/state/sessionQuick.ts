// An explicit off choice survives the default-on hover action recipe.
const KEY = "rx-session-quick-actions";
const listeners = new Set<() => void>();

export function showsSessionQuickActions(): boolean {
  try {
    return localStorage.getItem(KEY) !== "off";
  } catch {
    return true;
  }
}

export function setShowsSessionQuickActions(on: boolean): void {
  try {
    localStorage.setItem(KEY, on ? "on" : "off");
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  listeners.forEach((fn) => fn());
}

export function onSessionQuickActionsChange(fn: () => void): () => void {
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
