const KEY = "rx-settings-wide";
const listeners = new Set<() => void>();

export function settingsWide(): boolean {
  try {
    return localStorage.getItem(KEY) === "on";
  } catch {
    return false;
  }
}

export function onSettingsWideChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function setSettingsWide(on: boolean): void {
  try {
    if (on) localStorage.setItem(KEY, "on");
    else localStorage.removeItem(KEY);
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  listeners.forEach((fn) => fn());
}
